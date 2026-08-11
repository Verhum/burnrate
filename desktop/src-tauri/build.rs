fn main() {
    // Compile our Objective-C helper for requesting macOS notification authorization
    // via UNUserNotificationCenter (the modern API that triggers the permission dialog).
    // Explicit: tauri_build::build() emits its own rerun-if-changed set, which
    // replaces cargo's default "rerun when any package file changes". Without
    // this line, edits to the .m file are silently ignored and the stale object
    // is relinked.
    println!("cargo:rerun-if-changed=objc/notification_auth.m");

    cc::Build::new()
        .file("objc/notification_auth.m")
        .flag("-fobjc-arc")
        .compile("notification_auth");

    println!("cargo:rustc-link-lib=framework=UserNotifications");

    tauri_build::build();
}
