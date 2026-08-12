use std::fs;
use std::process::{Child, Command as StdCommand, Stdio};
use std::sync::Arc;
use std::time::Duration;

use futures_util::StreamExt;
use log::{error, info, warn};
use serde::{Deserialize, Serialize};
use tauri::image::Image;
use tauri::menu::{Menu, MenuBuilder, MenuItemBuilder};
use tauri::tray::{TrayIcon, TrayIconBuilder};
use tauri::{Manager, WebviewUrl, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_log::{RotationStrategy, Target, TargetKind, TimezoneStrategy};
use tauri_plugin_shell::ShellExt;
use tokio::sync::Mutex;

extern "C" {
    fn request_notification_authorization(callback: extern "C" fn(granted: i32));
}

extern "C" fn on_notification_auth_result(granted: i32) {
    if granted != 0 {
        log::info!("[notification] macOS notification permission granted");
    } else {
        log::warn!("[notification] macOS notification permission denied — notifications will not appear");
    }
}

const DEFAULT_PORT: u16 = 9112;
const TRAY_ID: &str = "burnrate-tray";

type SidecarChild = Arc<Mutex<Option<Child>>>;

fn sidecar_port() -> u16 {
    std::env::var("BURNRATE_PORT")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(DEFAULT_PORT)
}

fn enriched_path() -> String {
    let base = std::env::var("PATH").unwrap_or_default();
    let home = std::env::var("HOME").unwrap_or_else(|_| "/Users/unknown".into());
    let extra = [
        format!("{home}/.local/bin"),
        format!("{home}/.claude/local"),
        "/opt/homebrew/bin".into(),
        "/opt/homebrew/sbin".into(),
        format!("{home}/.cargo/bin"),
        "/usr/local/bin".into(),
    ];
    let mut parts: Vec<String> = extra.into_iter().collect();
    for p in base.split(':') {
        if !parts.contains(&p.to_string()) {
            parts.push(p.to_string());
        }
    }
    parts.join(":")
}

async fn wait_for_healthy(port: u16, polls: u32) -> Result<(), String> {
    let url = format!("http://127.0.0.1:{}/health", port);
    info!("[sidecar] polling {} for health", url);
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(2))
        .build()
        .map_err(|e| e.to_string())?;

    for i in 0..polls {
        match client.get(&url).send().await {
            Ok(resp) if resp.status().is_success() => {
                info!("[sidecar] healthy after {}ms", i * 500);
                return Ok(());
            }
            Ok(resp) => {
                info!("[sidecar] health poll {}: status={}", i, resp.status());
            }
            Err(e) => {
                if i % 5 == 0 {
                    info!("[sidecar] health poll {}: {}", i, e);
                }
            }
        }
        tokio::time::sleep(Duration::from_millis(500)).await;
    }
    Err(format!(
        "sidecar did not become healthy within {}s",
        polls / 2
    ))
}

#[derive(Debug, Clone, Serialize)]
struct HealthStatus {
    running: bool,
    port: u16,
}

#[tauri::command]
async fn health_status(child: tauri::State<'_, SidecarChild>) -> Result<HealthStatus, String> {
    let guard = child.lock().await;
    Ok(HealthStatus {
        running: guard.is_some(),
        port: sidecar_port(),
    })
}

fn show_main_window(app: &tauri::AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.set_focus();
    }
}

fn notify_frontend_ready(app: &tauri::AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let script = "window.dispatchEvent(new CustomEvent('burnrate-ready'));";
        info!("[nav] notifying frontend that sidecar is ready");
        if let Err(e) = w.eval(script) {
            error!("[nav] failed to notify frontend: {}", e);
        }
    }
}

fn find_sidecar_binary(app: &tauri::AppHandle) -> Option<String> {
    let target_name = if cfg!(target_arch = "aarch64") {
        "burnrate-aarch64-apple-darwin"
    } else {
        "burnrate-x86_64-apple-darwin"
    };
    info!("[sidecar] searching for binary (target={})", target_name);

    if let Ok(resource_dir) = app.path().resource_dir() {
        info!("[sidecar] resource_dir={}", resource_dir.display());
        for candidate in [
            resource_dir.join("binaries").join(target_name),
            resource_dir.join(target_name),
            resource_dir.join("burnrate"),
        ] {
            let exists = candidate.exists();
            info!("[sidecar]   candidate {} exists={}", candidate.display(), exists);
            if exists {
                return Some(candidate.to_string_lossy().into_owned());
            }
        }
    } else {
        warn!("[sidecar] could not resolve resource_dir");
    }

    if let Some(repo_root) = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(|p| p.parent())
    {
        let bin = repo_root.join("burnrate");
        let exists = bin.exists();
        info!("[sidecar]   dev fallback {} exists={}", bin.display(), exists);
        if exists {
            return Some(bin.to_string_lossy().into_owned());
        }
    }

    if let Ok(output) = StdCommand::new("which").arg("burnrate").output() {
        if output.status.success() {
            let path = String::from_utf8_lossy(&output.stdout).trim().to_string();
            if !path.is_empty() {
                info!("[sidecar]   which fallback: {}", path);
                return Some(path);
            }
        }
    }

    None
}

fn data_dir() -> String {
    if let Ok(d) = std::env::var("BURNRATE_DATA_DIR") {
        if !d.is_empty() {
            return d;
        }
    }
    let home = std::env::var("HOME").unwrap_or_else(|_| "/tmp".into());
    format!("{}/.burnrate", home)
}

fn parse_pids(out: &str) -> Vec<u32> {
    let me = std::process::id();
    let mut pids: Vec<u32> = out
        .lines()
        .filter_map(|l| l.trim().parse::<u32>().ok())
        .filter(|p| *p != me && *p > 1)
        .collect();
    pids.sort_unstable();
    pids.dedup();
    pids
}

/// PIDs listening on `port`. This is the authoritative "who owns the daemon
/// socket" answer — a leftover sidecar from a previous app run looks healthy
/// from out here but is not ours to supervise, and one that is wedged looks
/// like nothing at all.
fn pids_on_port(port: u16) -> Vec<u32> {
    // Absolute paths throughout this module: a Finder/launchd-launched .app
    // inherits a minimal PATH, and these helpers must not go silently blind
    // (an empty pid list reads as "nothing to kill") if it is emptier still.
    match StdCommand::new("/usr/sbin/lsof")
        .args([
            "-nP",
            &format!("-iTCP:{}", port),
            "-sTCP:LISTEN",
            "-t",
        ])
        .output()
    {
        Ok(out) => parse_pids(&String::from_utf8_lossy(&out.stdout)),
        Err(e) => {
            warn!("[sidecar] lsof failed: {}", e);
            Vec::new()
        }
    }
}

/// PIDs of `<binary_path> serve` processes — orphans from a previous run of
/// THIS app. Matching the full argv keeps a `go run ./cmd/burnrate serve` dev
/// daemon on another port out of the blast radius.
fn pids_running_binary(binary_path: &str) -> Vec<u32> {
    match StdCommand::new("/usr/bin/pgrep")
        .args(["-f", &format!("^{} serve", binary_path)])
        .output()
    {
        Ok(out) => parse_pids(&String::from_utf8_lossy(&out.stdout)),
        Err(e) => {
            warn!("[sidecar] pgrep failed: {}", e);
            Vec::new()
        }
    }
}

/// PID recorded in `<data_dir>/daemon.lock`. The Go daemon flocks that file and
/// exits if it cannot take the lock, so a stale holder blocks every start even
/// when the port itself is free. Only returned when the process is still alive
/// and still looks like a burnrate binary — the file outlives crashes and the
/// PID may have been recycled.
fn lockfile_pid(data_dir: &str) -> Option<u32> {
    let raw = fs::read_to_string(format!("{}/daemon.lock", data_dir)).ok()?;
    let pid: u32 = raw.trim().parse().ok()?;
    if pid <= 1 || pid == std::process::id() {
        return None;
    }
    let out = StdCommand::new("/bin/ps")
        .args(["-o", "command=", "-p", &pid.to_string()])
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let cmd = String::from_utf8_lossy(&out.stdout).to_lowercase();
    if cmd.contains("burnrate") && cmd.contains("serve") {
        Some(pid)
    } else {
        None
    }
}

fn signal_pid(pid: u32, sig: &str) {
    let _ = StdCommand::new("/bin/kill")
        .args([sig, &pid.to_string()])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

fn pid_alive(pid: u32) -> bool {
    StdCommand::new("/bin/kill")
        .args(["-0", &pid.to_string()])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

/// SIGTERM, wait up to 3s for a graceful exit, then SIGKILL.
async fn terminate_pids(pids: &[u32]) {
    if pids.is_empty() {
        return;
    }
    for &pid in pids {
        info!("[sidecar] SIGTERM pid={}", pid);
        signal_pid(pid, "-TERM");
    }
    for _ in 0..15 {
        if !pids.iter().any(|&p| pid_alive(p)) {
            return;
        }
        tokio::time::sleep(Duration::from_millis(200)).await;
    }
    for &pid in pids.iter().filter(|&&p| pid_alive(p)) {
        warn!("[sidecar] pid={} ignored SIGTERM, sending SIGKILL", pid);
        signal_pid(pid, "-KILL");
    }
    tokio::time::sleep(Duration::from_millis(300)).await;
}

/// Clear the way for a fresh sidecar: kill anything holding the port, the
/// data-dir lock, or running our own binary, then wait for the port to go
/// quiet. Adopting a pre-existing daemon instead (what this used to do) means
/// one wedged leftover process leaves the app permanently broken with no way
/// out but a manual `kill`.
async fn reclaim_sidecar_slot(port: u16, binary_path: &str) {
    let mut pids = pids_on_port(port);
    for pid in pids_running_binary(binary_path) {
        if !pids.contains(&pid) {
            pids.push(pid);
        }
    }
    if let Some(pid) = lockfile_pid(&data_dir()) {
        if !pids.contains(&pid) {
            pids.push(pid);
        }
    }

    if pids.is_empty() {
        info!("[sidecar] no existing daemon to stop");
        return;
    }
    info!("[sidecar] stopping existing daemon(s): {:?}", pids);
    terminate_pids(&pids).await;

    for i in 0..25 {
        if pids_on_port(port).is_empty() && !is_healthy(port).await {
            return;
        }
        if i == 0 {
            info!("[sidecar] waiting for port {} to free up", port);
        }
        tokio::time::sleep(Duration::from_millis(200)).await;
    }
    warn!(
        "[sidecar] port {} still occupied after kill — starting anyway",
        port
    );
}

/// Spawn `<binary_path> serve`, recording the child so we can kill it on exit.
async fn start_sidecar(binary_path: &str, port: u16, child: &SidecarChild) -> bool {
    let path_env = enriched_path();
    info!("[sidecar] starting: {} serve (port={})", binary_path, port);
    info!("[sidecar] PATH={}", path_env);

    let dir = data_dir();
    let _ = fs::create_dir_all(&dir);
    let log_path = format!("{}/desktop-sidecar.log", dir);
    info!("[sidecar] sidecar logs -> {}", log_path);

    let mut cmd = StdCommand::new(binary_path);
    cmd.arg("serve");
    cmd.env("BURNRATE_PORT", port.to_string());
    cmd.env("PATH", path_env);

    // Appended, not truncated: a retry must not erase the log that explains why
    // the previous attempt died.
    match fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&log_path)
    {
        Ok(log_file) => {
            let stderr_file = log_file
                .try_clone()
                .unwrap_or_else(|_| fs::File::create("/dev/null").unwrap());
            cmd.stdout(Stdio::from(log_file));
            cmd.stderr(Stdio::from(stderr_file));
        }
        Err(e) => {
            warn!("[sidecar] could not open log file {}: {}", log_path, e);
        }
    }

    match cmd.spawn() {
        Ok(spawned) => {
            info!("[sidecar] spawned pid={}", spawned.id());
            let mut guard = child.lock().await;
            *guard = Some(spawned);
            true
        }
        Err(e) => {
            error!("[sidecar] failed to spawn {}: {}", binary_path, e);
            false
        }
    }
}

/// Log why the child we spawned is not healthy — exited, or up but not serving.
async fn report_child_state(child: &SidecarChild) {
    let mut guard = child.lock().await;
    if let Some(ref mut c) = *guard {
        match c.try_wait() {
            Ok(Some(status)) => error!("[sidecar] process exited with {}", status),
            Ok(None) => error!("[sidecar] process still running but not healthy"),
            Err(e) => error!("[sidecar] could not check process: {}", e),
        }
    }
}

/// Kill + reap the child we spawned, synchronously, before a retry. (Unlike
/// [`kill_sidecar`], which fires and forgets from menu handlers.)
async fn kill_spawned_child(child: &SidecarChild) {
    let mut guard = child.lock().await;
    if let Some(ref mut c) = *guard {
        info!("[sidecar] killing our own child pid={} before retry", c.id());
        let _ = c.kill();
        let _ = c.wait();
    }
    *guard = None;
}

/// Surface a hard startup failure: the window otherwise sits there blank with
/// the reason buried in a log file.
fn sidecar_failed(app: &tauri::AppHandle, message: &str) {
    if let Some(w) = app.get_webview_window("main") {
        let payload = serde_json::json!({ "message": message }).to_string();
        let script = format!(
            "window.dispatchEvent(new CustomEvent('burnrate-sidecar-failed', {{ detail: {} }}));",
            payload
        );
        let _ = w.eval(&script);
        let _ = w.show();
    }
    let _ = tauri_plugin_notification::NotificationExt::notification(app)
        .builder()
        .title("Burnrate failed to start")
        .body(message)
        .show();
}

fn kill_sidecar(child: &SidecarChild) {
    let child = child.clone();
    tauri::async_runtime::spawn(async move {
        let mut guard = child.lock().await;
        if let Some(ref mut c) = *guard {
            info!("[sidecar] killing child");
            let _ = c.kill();
            let _ = c.wait();
        }
        *guard = None;
    });
}

// --- Tray status types ---

#[derive(Debug, Deserialize, Default, Clone)]
struct ApiStatus {
    window_state: Option<String>,
    five_hour_util: Option<f64>,
    seven_day_util: Option<f64>,
    seven_day_resets_at: Option<String>,
    running_count: Option<i64>,
    running_runs: Option<Vec<RunningRun>>,
    captured_at: Option<String>,
    limits: Option<Vec<ApiLimit>>,
    /// Human-loop requests waiting on the user. Absent on daemons older than the
    /// feature, which must keep behaving exactly as before.
    pending_request_count: Option<u64>,
}

#[derive(Debug, Deserialize, Clone)]
struct RunningRun {
    title: Option<String>,
    run_id: Option<i64>,
    started_at: Option<String>,
}

#[derive(Debug, Deserialize, Clone)]
struct ApiLimit {
    kind: Option<String>,
    is_active: Option<bool>,
}

#[derive(Debug, Deserialize, Default, Clone)]
struct ApiCaffeinate {
    active: Option<bool>,
    reason: Option<String>,
    uptime: Option<String>,
    manual: Option<bool>,
}

#[derive(Debug, Clone, Copy, PartialEq)]
enum UsageLevel {
    Low,
    Mid,
    High,
    Limited,
    Unknown,
}

fn usage_level(util: f64) -> UsageLevel {
    if util < 60.0 {
        UsageLevel::Low
    } else if util < 80.0 {
        UsageLevel::Mid
    } else {
        UsageLevel::High
    }
}

fn session_limit_hit(status: &ApiStatus) -> bool {
    status.limits.as_ref().map_or(false, |limits| {
        limits.iter().any(|l| {
            l.kind.as_deref() == Some("session") && l.is_active.unwrap_or(false)
        })
    })
}

/// The tray disc, coloured by usage level — except when a human request is
/// waiting, which overrides the level with blue and punches a hole in the middle
/// so the state also reads without colour.
fn generate_tray_icon(level: UsageLevel, pending: bool) -> Image<'static> {
    let (r, g, b) = if pending {
        (47u8, 129u8, 247u8)
    } else {
        match level {
            UsageLevel::Low => (63u8, 185u8, 80u8),
            UsageLevel::Mid => (210u8, 153u8, 34u8),
            UsageLevel::High => (248u8, 81u8, 73u8),
            UsageLevel::Limited => (239u8, 68u8, 68u8),
            UsageLevel::Unknown => (139u8, 148u8, 158u8),
        }
    };

    let size: u32 = 22;
    let mut rgba = vec![0u8; (size * size * 4) as usize];

    let center = size as f64 / 2.0;
    let radius = (size as f64 / 2.0) - 1.0;
    let hole = if pending { radius * 0.38 } else { 0.0 };

    for y in 0..size {
        for x in 0..size {
            let dx = x as f64 - center;
            let dy = y as f64 - center;
            let dist = (dx * dx + dy * dy).sqrt();

            let idx = ((y * size + x) * 4) as usize;
            if dist <= radius && dist >= hole {
                let mut alpha = if dist > radius - 1.0 {
                    ((radius - dist).max(0.0) * 255.0) as u8
                } else {
                    255
                };
                if hole > 0.0 && dist < hole + 1.0 {
                    alpha = alpha.min(((dist - hole).max(0.0) * 255.0) as u8);
                }
                rgba[idx] = r;
                rgba[idx + 1] = g;
                rgba[idx + 2] = b;
                rgba[idx + 3] = alpha;
            }
        }
    }

    Image::new_owned(rgba, size, size)
}

struct TrayState {
    last_level: UsageLevel,
    last_pending: bool,
    last_fingerprint: String,
}

/// Wall-clock label for a reset instant, e.g. "Wed 4am" / "Wed 4:30am".
/// Mirrors `formatResetDay` in web/src/lib/format.ts.
fn reset_day_label(iso: &str) -> Option<String> {
    let dt = chrono::DateTime::parse_from_rfc3339(iso)
        .ok()?
        .with_timezone(&chrono::Local);
    let on_the_hour = dt.format("%M").to_string() == "00";
    let fmt = if on_the_hour { "%a %-l%P" } else { "%a %-l:%M%P" };
    Some(dt.format(fmt).to_string())
}

/// Local wall-clock label for a run's start, e.g. "2:14pm", or "Jul 24 2:14pm"
/// once the run has outlived the day it started on. Mirrors `formatStartTime`
/// in web/src/lib/format.ts so the tray and the window agree.
fn run_start_label(iso: &str) -> Option<String> {
    let dt = chrono::DateTime::parse_from_rfc3339(iso)
        .ok()?
        .with_timezone(&chrono::Local);
    let fmt = if dt.date_naive() == chrono::Local::now().date_naive() {
        "%-l:%M%P"
    } else {
        "%b %-e %-l:%M%P"
    };
    Some(dt.format(fmt).to_string())
}

/// Coarse countdown to a reset instant, e.g. "2d 5h". Mirrors
/// `formatLongCountdown` in web/src/lib/format.ts.
fn reset_countdown(iso: &str) -> Option<String> {
    let dt = chrono::DateTime::parse_from_rfc3339(iso).ok()?;
    let secs = (dt.with_timezone(&chrono::Utc) - chrono::Utc::now()).num_seconds();
    if secs <= 0 {
        return Some("now".to_string());
    }
    let (d, h, m) = (secs / 86400, (secs % 86400) / 3600, (secs % 3600) / 60);
    Some(if d > 0 {
        format!("{}d {}h", d, h)
    } else if h > 0 {
        format!("{}h {}m", h, m)
    } else {
        format!("{}m", m)
    })
}

/// Tray label for waiting human-loop requests, e.g. "3 requests waiting for you".
fn pending_requests_label(count: u64) -> String {
    format!(
        "{} request{} waiting for you",
        count,
        if count == 1 { "" } else { "s" }
    )
}

fn rebuild_tray_menu(
    app: &tauri::AppHandle,
    tray: &TrayIcon,
    status: &ApiStatus,
    caff: &ApiCaffeinate,
    child_for_tray: &SidecarChild,
) {
    let five_h = status.five_hour_util.unwrap_or(0.0);
    let seven_d = status.seven_day_util.unwrap_or(0.0);
    let window = status.window_state.as_deref().unwrap_or("IDLE");
    let running = status.running_count.unwrap_or(0);
    let pending_requests = status.pending_request_count.unwrap_or(0);
    let caff_active = caff.active.unwrap_or(false);

    let caff_text = if caff_active {
        let mode = if caff.manual.unwrap_or(false) {
            "manual"
        } else {
            "auto"
        };
        let uptime_str = caff.uptime.as_deref().unwrap_or("");
        if !uptime_str.is_empty() && uptime_str != "0s" {
            format!("Caffeinate: ON — {} ({})", mode, uptime_str)
        } else {
            format!("Caffeinate: ON — {}", mode)
        }
    } else {
        "Caffeinate: OFF — click to enable".to_string()
    };

    let child_clone = child_for_tray.clone();

    let Ok(u5) = MenuItemBuilder::with_id("u5", format!("5h:  {:.1}%", five_h)).enabled(false).build(app) else { return };
    let seven_d_text = match status.seven_day_resets_at.as_deref().and_then(reset_day_label) {
        Some(label) => {
            let countdown = status
                .seven_day_resets_at
                .as_deref()
                .and_then(reset_countdown)
                .unwrap_or_default();
            format!("7d:  {:.1}%  — resets {} ({})", seven_d, label, countdown)
        }
        None => format!("7d:  {:.1}%", seven_d),
    };
    let Ok(u7) = MenuItemBuilder::with_id("u7", seven_d_text).enabled(false).build(app) else { return };
    let Ok(win) = MenuItemBuilder::with_id("win", format!("Window: {}", window)).enabled(false).build(app) else { return };

    let mut running_text = format!("Running: {}", running);
    if let Some(runs) = &status.running_runs {
        for rr in runs.iter().take(3) {
            if let Some(title) = &rr.title {
                let short = if title.len() > 30 { format!("{}...", &title[..27]) } else { title.clone() };
                let started = rr
                    .started_at
                    .as_deref()
                    .and_then(run_start_label)
                    .map(|t| format!("  started {}", t))
                    .unwrap_or_default();
                running_text.push_str(&format!("\n  #{} {}{}", rr.run_id.unwrap_or(0), short, started));
            }
        }
    }
    let Ok(run_item) = MenuItemBuilder::with_id("run", running_text).enabled(false).build(app) else { return };
    let Ok(caff_item) = MenuItemBuilder::with_id("caffeinate", caff_text).build(app) else { return };

    let staleness_text = if let Some(ref ts) = status.captured_at {
        if let Ok(dt) = chrono::DateTime::parse_from_rfc3339(ts) {
            format!(
                "Usage as of {}",
                dt.with_timezone(&chrono::Local).format("%H:%M:%S")
            )
        } else {
            "Usage as of --:--:--".to_string()
        }
    } else {
        "Usage as of --:--:--".to_string()
    };
    let Ok(staleness_item) = MenuItemBuilder::with_id("staleness", staleness_text).enabled(false).build(app) else { return };

    let Ok(show_item) = MenuItemBuilder::with_id("show", "Show Window").build(app) else { return };
    let Ok(quit_item) = MenuItemBuilder::with_id("quit", "Quit Burnrate").build(app) else { return };

    // Waiting requests go above everything: they are the only tray item that is a
    // request for the human rather than a report to them.
    let requests_item = if pending_requests > 0 {
        match MenuItemBuilder::with_id("requests", pending_requests_label(pending_requests))
            .build(app)
        {
            Ok(item) => Some(item),
            Err(_) => return,
        }
    } else {
        None
    };

    let mut builder = MenuBuilder::new(app);
    if let Some(item) = &requests_item {
        builder = builder.item(item).separator();
    }

    let Ok(menu) = builder
        .item(&u5)
        .item(&u7)
        .separator()
        .item(&win)
        .item(&run_item)
        .item(&caff_item)
        .separator()
        .item(&staleness_item)
        .separator()
        .item(&show_item)
        .separator()
        .item(&quit_item)
        .build()
    else { return };

    let _ = tray.set_menu(Some(menu));

    tray.on_menu_event(move |app, event| match event.id().as_ref() {
        "show" | "requests" => show_main_window(app),
        "caffeinate" => {
            let port = sidecar_port();
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                let url = format!("http://127.0.0.1:{}/api/caffeinate", port);
                let client = reqwest::Client::new();
                match client.post(&url).send().await {
                    Ok(_) => info!("[tray] toggled caffeinate"),
                    Err(e) => warn!("[tray] failed to toggle caffeinate: {}", e),
                }
                let _ = poll_and_update_tray(&app, port).await;
            });
        }
        "quit" => {
            kill_sidecar(&child_clone);
            app.exit(0);
        }
        _ => {}
    });

    let weekly_reset = status
        .seven_day_resets_at
        .as_deref()
        .and_then(reset_day_label)
        .map(|l| format!(" | 7d resets {}", l))
        .unwrap_or_default();
    let requests_suffix = if pending_requests > 0 {
        format!(" | {}", pending_requests_label(pending_requests))
    } else {
        String::new()
    };
    let tooltip = format!(
        "Burnrate — 5h: {:.0}% | 7d: {:.0}%{} | {} | {} running{}",
        five_h, seven_d, weekly_reset, window, running, requests_suffix
    );
    let _ = tray.set_tooltip(Some(&tooltip));
}

async fn try_start_sidecar(
    handle: &tauri::AppHandle,
    binary_path: &str,
    port: u16,
    child: &SidecarChild,
) -> bool {
    for attempt in 1..=5u32 {
        if attempt > 1 {
            warn!("[sidecar] start attempt {} of 5", attempt);
            kill_spawned_child(child).await;
            tokio::time::sleep(Duration::from_millis(500 * attempt as u64)).await;
        }
        reclaim_sidecar_slot(port, binary_path).await;
        if start_sidecar(binary_path, port, child).await
            && wait_for_healthy(port, 60).await.is_ok()
        {
            notify_frontend_ready(handle);
            return true;
        }
        report_child_state(child).await;
    }
    error!("[sidecar] gave up after 5 attempts — check logs at ~/.burnrate/desktop-sidecar.log");
    sidecar_failed(
        handle,
        "Burnrate could not start its background daemon. See ~/.burnrate/desktop-sidecar.log.",
    );
    false
}

pub fn run() {
    let child_state: SidecarChild = Arc::new(Mutex::new(None));
    let child_for_exit = child_state.clone();

    tauri::Builder::default()
        .plugin({
            let home = std::env::var("HOME").unwrap_or_else(|_| "/tmp".into());
            let burnrate_log = format!("{}/.burnrate/desktop-tauri.log", home);
            let _ = fs::create_dir_all(format!("{}/.burnrate", home));

            let mut targets = vec![
                Target::new(TargetKind::LogDir { file_name: None }),
                Target::new(TargetKind::Stderr),
            ];
            if let Ok(f) = std::fs::File::create(&burnrate_log) {
                drop(f);
                targets.push(Target::new(TargetKind::Folder {
                    path: std::path::PathBuf::from(format!("{}/.burnrate", home)),
                    file_name: Some("desktop-tauri".into()),
                }));
            }

            tauri_plugin_log::Builder::new()
                .targets(targets)
                .rotation_strategy(RotationStrategy::KeepOne)
                .max_file_size(5_000_000)
                .timezone_strategy(TimezoneStrategy::UseLocal)
                .level(log::LevelFilter::Info)
                .level_for("burnrate_desktop_lib", log::LevelFilter::Debug)
                .build()
        })
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .invoke_handler(tauri::generate_handler![health_status])
        .menu(Menu::default)
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                let _ = window.hide();
                api.prevent_close();
            }
        })
        .setup(move |app| {
            let handle = app.handle().clone();
            app.manage(child_state.clone());

            // -- System tray --
            let child_for_tray = child_state.clone();
            let show_item = MenuItemBuilder::with_id("show", "Show Window").build(app)?;
            let caff_item = MenuItemBuilder::with_id("caffeinate", "Caffeinate: --").build(app)?;
            let quit_item = MenuItemBuilder::with_id("quit", "Quit Burnrate").build(app)?;
            let tray_menu = MenuBuilder::new(app)
                .item(&show_item)
                .item(&caff_item)
                .separator()
                .item(&quit_item)
                .build()?;

            let tray_icon = TrayIconBuilder::with_id(TRAY_ID)
                .icon(generate_tray_icon(UsageLevel::Unknown, false))
                .icon_as_template(false)
                .tooltip("Burnrate")
                .menu(&tray_menu)
                .on_menu_event(move |app, event| match event.id().as_ref() {
                    "show" => show_main_window(app),
                    "caffeinate" => {
                        let port = sidecar_port();
                        let app = app.clone();
                        tauri::async_runtime::spawn(async move {
                            let url = format!("http://127.0.0.1:{}/api/caffeinate", port);
                            let client = reqwest::Client::new();
                            match client.post(&url).send().await {
                                Ok(_) => info!("[tray] toggled caffeinate"),
                                Err(e) => warn!("[tray] failed to toggle caffeinate: {}", e),
                            }
                            let _ = poll_and_update_tray(&app, port).await;
                        });
                    }
                    "quit" => {
                        kill_sidecar(&child_for_tray);
                        app.exit(0);
                    }
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let tauri::tray::TrayIconEvent::Click {
                        button: tauri::tray::MouseButton::Left,
                        button_state: tauri::tray::MouseButtonState::Up,
                        ..
                    } = event
                    {
                        show_main_window(tray.app_handle());
                    }
                })
                .build(app)?;

            app.manage(tray_icon);
            app.manage(Arc::new(Mutex::new(TrayState {
                last_level: UsageLevel::Unknown,
                last_pending: false,
                last_fingerprint: String::new(),
            })));

            let nav_handle = app.handle().clone();
            WebviewWindowBuilder::new(app.handle(), "main", WebviewUrl::App("index.html".into()))
                .title("Burnrate")
                .inner_size(960.0, 700.0)
                .min_inner_size(720.0, 500.0)
                .center()
                // The embedded UI cannot know which port the daemon was told to
                // use, and it talks to 127.0.0.1 directly (no dev-server proxy),
                // so BURNRATE_PORT has to reach the webview too — otherwise
                // moving the daemon off 9112 just points the UI at nothing.
                // Read by detectBaseUrl() in web/src/lib/api/client.ts.
                .initialization_script(&format!(
                    "window.__BURNRATE_PORT__ = {};",
                    sidecar_port()
                ))
                .on_navigation(move |url| {
                    match url.scheme() {
                        "tauri" | "data" | "blob" | "about" => true,
                        "http" | "https" => match url.host_str() {
                            Some("127.0.0.1") | Some("localhost") | Some("tauri.localhost") => true,
                            _ => {
                                info!("[nav] opening external URL in browser: {}", url);
                                #[allow(deprecated)]
                                let _ = nav_handle.shell().open(url.as_str(), None);
                                false
                            }
                        },
                        // The webview has no mail client; without this the Help
                        // link is a dead click whenever the frontend's own
                        // interceptor doesn't get to it first.
                        "mailto" => {
                            info!("[nav] opening mail client: {}", url);
                            #[allow(deprecated)]
                            let _ = nav_handle.shell().open(url.as_str(), None);
                            false
                        }
                        _ => false,
                    }
                })
                .build()?;

            // -- Start sidecar --
            let child_for_spawn = child_state.clone();
            let port = sidecar_port();

            let handle_for_sidecar = handle.clone();
            let handle_for_notify = handle.clone();
            tauri::async_runtime::spawn(async move {
                let handle = handle_for_sidecar;

                let binary_path = match find_sidecar_binary(&handle) {
                    Some(p) => p,
                    None => {
                        error!("[sidecar] burnrate binary not found — build it with `go build -o burnrate ./cmd/burnrate` in the repo root");
                        sidecar_failed(&handle, "Burnrate's daemon binary is missing from the app bundle.");
                        return;
                    }
                };

                if !try_start_sidecar(&handle, &binary_path, port, &child_for_spawn).await {
                    return;
                }

                start_tray_poller(handle.clone(), port);

                // Watchdog: if the daemon dies while the app is open, restart it.
                loop {
                    tokio::time::sleep(Duration::from_secs(5)).await;
                    let alive = {
                        let mut guard = child_for_spawn.lock().await;
                        match *guard {
                            Some(ref mut c) => match c.try_wait() {
                                Ok(Some(status)) => {
                                    warn!("[watchdog] sidecar exited with {}", status);
                                    *guard = None;
                                    false
                                }
                                Ok(None) => true,
                                Err(e) => {
                                    warn!("[watchdog] could not check sidecar: {}", e);
                                    false
                                }
                            },
                            None => false,
                        }
                    };
                    if !alive && !is_healthy(port).await {
                        warn!("[watchdog] sidecar is down, restarting");
                        if !try_start_sidecar(&handle, &binary_path, port, &child_for_spawn).await {
                            return;
                        }
                    }
                }
            });

            // -- Request macOS notification permission --
            // UNUserNotificationCenter.requestAuthorization triggers the system
            // permission dialog on first launch. Subsequent calls are no-ops.
            unsafe { request_notification_authorization(on_notification_auth_result) };

            // -- Start notification listener --
            let notify_handle = handle_for_notify;
            tauri::async_runtime::spawn(async move {
                start_notification_listener(notify_handle, port).await;
            });

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error building burnrate desktop app")
        .run(move |_app, event| {
            if let tauri::RunEvent::Exit = event {
                let child = child_for_exit.clone();
                let rt = tokio::runtime::Builder::new_current_thread()
                    .enable_all()
                    .build();
                if let Ok(rt) = rt {
                    rt.block_on(async {
                        let mut guard = child.lock().await;
                        if let Some(ref mut c) = *guard {
                            info!("[sidecar] killing on app exit");
                            let _ = c.kill();
                            let _ = c.wait();
                        }
                        *guard = None;
                    });
                }
            }
        });
}

fn start_tray_poller(app: tauri::AppHandle, port: u16) {
    tauri::async_runtime::spawn(async move {
        loop {
            tokio::time::sleep(Duration::from_secs(3)).await;
            let _ = poll_and_update_tray(&app, port).await;
        }
    });
}

async fn poll_and_update_tray(app: &tauri::AppHandle, port: u16) -> Result<(), ()> {
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .map_err(|_| ())?;

    let status_url = format!("http://127.0.0.1:{}/api/status", port);
    let caff_url = format!("http://127.0.0.1:{}/api/caffeinate", port);

    let (status_res, caff_res) = tokio::join!(
        client.get(&status_url).send(),
        client.get(&caff_url).send()
    );

    let status: ApiStatus = match status_res {
        Ok(r) => r.json().await.unwrap_or_default(),
        Err(_) => ApiStatus::default(),
    };
    let caff: ApiCaffeinate = match caff_res {
        Ok(r) => r.json().await.unwrap_or_default(),
        Err(_) => ApiCaffeinate::default(),
    };

    let five_h = status.five_hour_util.unwrap_or(0.0);
    let level = if session_limit_hit(&status) {
        UsageLevel::Limited
    } else {
        usage_level(five_h)
    };
    let pending = status.pending_request_count.unwrap_or(0) > 0;

    let fingerprint = format!(
        "{}|{}|{}|{}|{}|{}|{}|{}|{}|{}",
        status.five_hour_util.unwrap_or(0.0),
        status.seven_day_util.unwrap_or(0.0),
        status.seven_day_resets_at.as_deref().unwrap_or(""),
        status.window_state.as_deref().unwrap_or(""),
        status.running_count.unwrap_or(0),
        caff.active.unwrap_or(false),
        caff.reason.as_deref().unwrap_or(""),
        status.captured_at.as_deref().unwrap_or(""),
        session_limit_hit(&status),
        status.pending_request_count.unwrap_or(0),
    );

    if let Some(tray) = app.tray_by_id(TRAY_ID) {
        let tray_state: tauri::State<'_, Arc<Mutex<TrayState>>> = app.state();
        let mut state = tray_state.lock().await;

        if fingerprint != state.last_fingerprint {
            let child_state: tauri::State<'_, SidecarChild> = app.state();
            rebuild_tray_menu(app, &tray, &status, &caff, &*child_state);
            state.last_fingerprint = fingerprint;
        }

        if level != state.last_level || pending != state.last_pending {
            let _ = tray.set_icon(Some(generate_tray_icon(level, pending)));
            let _ = tray.set_icon_as_template(false);
            state.last_level = level;
            state.last_pending = pending;
        }
    }

    Ok(())
}

async fn is_healthy(port: u16) -> bool {
    let url = format!("http://127.0.0.1:{}/health", port);
    match reqwest::get(&url).await {
        Ok(resp) => resp.status().is_success(),
        Err(_) => false,
    }
}

/// Payload of the SSE `notification` event. `task_id`/`request_id` are absent
/// whenever the notification is not about a specific task or human request.
#[derive(Debug, Deserialize)]
struct NotificationEvent {
    title: Option<String>,
    body: Option<String>,
    task_id: Option<u64>,
    request_id: Option<u64>,
}

/// Prefix the body with the task reference, so a notification that cannot be
/// clicked through still says where to go.
fn notification_body(ev: &NotificationEvent) -> String {
    let body = ev.body.as_deref().unwrap_or("");
    match ev.task_id {
        Some(id) => format!("BR{}: {}", id, body),
        None => body.to_string(),
    }
}

/// Show one SSE notification as a native macOS notification.
///
/// There is no click-through: tauri-plugin-notification v2 exposes activation
/// callbacks on mobile only (`register_action_types` is `#[cfg(mobile)]`), and
/// its desktop path hands the notification to notify-rust and immediately drops
/// the handle that `wait_for_action` would need. Deep-linking a click to the
/// task would mean bypassing the plugin, so for now the body carries the task
/// reference and tray left-click is the way back to the window.
fn show_notification(app: &tauri::AppHandle, ev: &NotificationEvent) {
    let title = ev.title.as_deref().unwrap_or("Burnrate");
    let body = notification_body(ev);
    info!(
        "[notification] showing: {} (task_id={:?} request_id={:?})",
        title, ev.task_id, ev.request_id
    );
    let _ = tauri_plugin_notification::NotificationExt::notification(app)
        .builder()
        .title(title)
        .body(&body)
        .show();
}

async fn start_notification_listener(app: tauri::AppHandle, port: u16) {
    let url = format!("http://127.0.0.1:{}/api/events", port);
    // No total timeout: `timeout` is a deadline on the *whole* request, which for
    // an SSE stream means "kill it mid-stream". `Duration::from_secs(0)` used to
    // sit here labelled "no timeout" — reqwest read it as an instant deadline, so
    // every connection died on arrival and this loop just reconnect-churned every
    // 5s and never delivered a notification. reqwest has no total timeout by
    // default; only the connect phase gets one, so a dead daemon still fails fast.
    let client = reqwest::Client::builder()
        .connect_timeout(Duration::from_secs(10))
        .build()
        .unwrap_or_default();

    let mut backoff_secs: u64 = 2;
    const MAX_BACKOFF: u64 = 30;

    loop {
        match client.get(&url).send().await {
            Ok(resp) => {
                info!("[notification] SSE connection established");
                backoff_secs = 2;
                let mut stream = resp.bytes_stream();
                let mut buf = String::new();
                let mut event_type = String::new();
                while let Some(chunk) = stream.next().await {
                    match chunk {
                        Ok(bytes) => {
                            if let Ok(text) = String::from_utf8(bytes.to_vec()) {
                                buf.push_str(&text);
                                while let Some(pos) = buf.find('\n') {
                                    let line: String = buf.drain(..=pos).collect();
                                    let line = line.trim();
                                    if line.is_empty() {
                                        event_type.clear();
                                    } else if let Some(et) = line.strip_prefix("event: ") {
                                        event_type = et.to_string();
                                    } else if let Some(data) = line.strip_prefix("data: ") {
                                        if event_type == "notification" {
                                            match serde_json::from_str::<NotificationEvent>(data) {
                                                Ok(ev) => show_notification(&app, &ev),
                                                Err(e) => warn!(
                                                    "[notification] unparseable payload ({}): {}",
                                                    e, data
                                                ),
                                            }
                                        }
                                    }
                                }
                            }
                        }
                        Err(e) => {
                            warn!("[notification] SSE stream error: {:?}", e);
                            break;
                        }
                    }
                }
                info!("[notification] SSE connection closed, reconnecting in {}s", backoff_secs);
            }
            Err(e) => {
                warn!("[notification] SSE connection failed (retry in {}s): {:?}", backoff_secs, e);
            }
        }
        tokio::time::sleep(Duration::from_secs(backoff_secs)).await;
        backoff_secs = (backoff_secs * 2).min(MAX_BACKOFF);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::{TimeZone, Timelike};

    /// Build an RFC3339 string for a local wall-clock time, so the label
    /// assertions below hold regardless of the machine's timezone.
    fn local_rfc3339(h: u32, m: u32) -> String {
        // 2026-07-29 is a Wednesday.
        chrono::Local
            .with_ymd_and_hms(2026, 7, 29, h, m, 0)
            .unwrap()
            .to_rfc3339()
    }

    #[test]
    fn parse_pids_filters_self_and_junk() {
        let me = std::process::id();
        let out = format!("123\n{}\n1\n\nnot-a-pid\n123\n456\n", me);
        // Own pid excluded (we would kill the app), pid 1 excluded (launchd),
        // duplicates collapsed, garbage dropped.
        assert_eq!(parse_pids(&out), vec![123, 456]);
        assert!(parse_pids("").is_empty());
    }

    #[test]
    fn reset_day_label_drops_zero_minutes() {
        assert_eq!(reset_day_label(&local_rfc3339(4, 0)).as_deref(), Some("Wed 4am"));
        assert_eq!(reset_day_label(&local_rfc3339(0, 0)).as_deref(), Some("Wed 12am"));
        assert_eq!(reset_day_label(&local_rfc3339(16, 0)).as_deref(), Some("Wed 4pm"));
    }

    #[test]
    fn reset_day_label_keeps_nonzero_minutes() {
        assert_eq!(reset_day_label(&local_rfc3339(4, 30)).as_deref(), Some("Wed 4:30am"));
        assert_eq!(reset_day_label(&local_rfc3339(16, 5)).as_deref(), Some("Wed 4:05pm"));
    }

    #[test]
    fn reset_day_label_rejects_garbage() {
        assert_eq!(reset_day_label("not-a-time"), None);
        assert_eq!(reset_day_label(""), None);
    }

    #[test]
    fn run_start_label_drops_the_date_for_today() {
        let today = chrono::Local::now()
            .with_hour(14)
            .unwrap()
            .with_minute(14)
            .unwrap()
            .with_second(0)
            .unwrap();
        assert_eq!(
            run_start_label(&today.to_rfc3339()).as_deref(),
            Some("2:14pm")
        );
    }

    #[test]
    fn run_start_label_keeps_the_date_for_an_earlier_day() {
        let earlier = chrono::Local::now() - chrono::Duration::days(2);
        let earlier = earlier
            .with_hour(9)
            .unwrap()
            .with_minute(5)
            .unwrap()
            .with_second(0)
            .unwrap();
        let want = earlier.format("%b %-e 9:05am").to_string();
        assert_eq!(run_start_label(&earlier.to_rfc3339()).as_deref(), Some(want.as_str()));
    }

    #[test]
    fn run_start_label_rejects_garbage() {
        assert_eq!(run_start_label("not-a-time"), None);
        assert_eq!(run_start_label(""), None);
    }

    #[test]
    fn pending_requests_label_pluralizes() {
        assert_eq!(pending_requests_label(1), "1 request waiting for you");
        assert_eq!(pending_requests_label(3), "3 requests waiting for you");
    }

    #[test]
    fn notification_body_prefixes_the_task_reference() {
        let ev: NotificationEvent =
            serde_json::from_str(r#"{"title":"t","body":"check the modal","task_id":42}"#).unwrap();
        assert_eq!(ev.request_id, None);
        assert_eq!(notification_body(&ev), "BR42: check the modal");
    }

    #[test]
    fn notification_body_tolerates_the_old_two_field_payload() {
        let ev: NotificationEvent = serde_json::from_str(r#"{"title":"t","body":"b"}"#).unwrap();
        assert_eq!(ev.task_id, None);
        assert_eq!(notification_body(&ev), "b");
    }

    #[test]
    fn pending_tray_icon_differs_from_the_usage_coloured_one() {
        let plain = generate_tray_icon(UsageLevel::Low, false);
        let pending = generate_tray_icon(UsageLevel::Low, true);
        assert_ne!(plain.rgba(), pending.rgba());
        // The hole makes the centre transparent, so the state reads without colour.
        let size = pending.width();
        let centre = (((size / 2) * size + size / 2) * 4 + 3) as usize;
        assert_eq!(pending.rgba()[centre], 0);
        assert_eq!(plain.rgba()[centre], 255);
    }

    #[test]
    fn status_without_pending_count_reads_as_zero() {
        let status: ApiStatus = serde_json::from_str(r#"{"five_hour_util":10.0}"#).unwrap();
        assert_eq!(status.pending_request_count, None);
    }

    #[test]
    fn reset_countdown_buckets_by_magnitude() {
        let at = |secs: i64| {
            (chrono::Utc::now() + chrono::Duration::seconds(secs) + chrono::Duration::seconds(1))
                .to_rfc3339()
        };
        assert_eq!(reset_countdown(&at(2 * 86400 + 5 * 3600)).as_deref(), Some("2d 5h"));
        assert_eq!(reset_countdown(&at(5 * 3600 + 30 * 60)).as_deref(), Some("5h 30m"));
        assert_eq!(reset_countdown(&at(90)).as_deref(), Some("1m"));
        assert_eq!(reset_countdown(&at(-3600)).as_deref(), Some("now"));
        assert_eq!(reset_countdown("not-a-time"), None);
    }
}
