import type {
  Task,
  TaskStats,
  Run,
  RunResume,
  Comment,
  Attachment,
  UsageSnapshot,
  StatusInfo,
  CaffeinateStatus,
  AccountsResponse,
  Config,
  LogEvent,
  LeaderboardData,
  CostEfficiency,
  StreakData,
  AchievementsData,
  CreateTaskRequest,
  UpdateTaskRequest,
  ChangeStatusRequest,
  ReorderTasksRequest,
  CreateCommentRequest,
  SelectAccountRequest,
  CheckoutResult,
  TaskPR,
  ModelInfo,
  HumanRequest,
  RequestResult,
  Capture,
} from "./types";

export class BurnrateClient {
  private baseUrl: string;

  constructor(baseUrl: string = "") {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const opts: RequestInit = {
      method,
      headers: { "Content-Type": "application/json" },
    };
    if (body !== undefined) {
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(url, opts);
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new ApiError(res.status, text || res.statusText);
    }
    return res.json() as Promise<T>;
  }

  // --- Tasks ---

  async listTasks(): Promise<Task[]> {
    return this.request<Task[]>("GET", "/api/tasks");
  }

  async createTask(req: CreateTaskRequest): Promise<Task> {
    return this.request<Task>("POST", "/api/tasks", req);
  }

  async updateTask(id: number, req: UpdateTaskRequest): Promise<Task> {
    return this.request<Task>("PUT", `/api/tasks/${id}`, req);
  }

  async deleteTask(id: number): Promise<void> {
    await this.request<unknown>("DELETE", `/api/tasks/${id}`);
  }

  async changeTaskStatus(id: number, req: ChangeStatusRequest): Promise<void> {
    await this.request<unknown>("POST", `/api/tasks/${id}/status`, req);
  }

  async runTaskNow(id: number): Promise<void> {
    await this.request<unknown>("POST", `/api/tasks/${id}/run-now`);
  }

  async getTaskStats(): Promise<Record<number, TaskStats>> {
    return this.request<Record<number, TaskStats>>("GET", "/api/tasks/stats");
  }

  async reorderTasks(req: ReorderTasksRequest): Promise<void> {
    await this.request<unknown>("POST", "/api/tasks/reorder", req);
  }

  // --- Comments ---

  async listComments(taskId: number): Promise<Comment[]> {
    return this.request<Comment[]>("GET", `/api/tasks/${taskId}/comments`);
  }

  async createComment(
    taskId: number,
    req: CreateCommentRequest
  ): Promise<Comment> {
    return this.request<Comment>(
      "POST",
      `/api/tasks/${taskId}/comments`,
      req
    );
  }

  // --- Attachments ---

  async listAttachments(taskId: number): Promise<Attachment[]> {
    return this.request<Attachment[]>(
      "GET",
      `/api/tasks/${taskId}/attachments`
    );
  }

  async uploadAttachment(taskId: number, file: File): Promise<Attachment> {
    const url = `${this.baseUrl}/api/tasks/${taskId}/attachments`;
    const formData = new FormData();
    formData.append("file", file);
    const res = await fetch(url, { method: "POST", body: formData });
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new ApiError(res.status, text || res.statusText);
    }
    return res.json() as Promise<Attachment>;
  }

  async deleteAttachment(id: number): Promise<void> {
    const url = `${this.baseUrl}/api/attachments/${id}`;
    const res = await fetch(url, { method: "DELETE" });
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new ApiError(res.status, text || res.statusText);
    }
  }

  attachmentDataUrl(id: number): string {
    return `${this.baseUrl}/api/attachments/${id}/data`;
  }

  // --- Pull requests ---

  async checkoutTask(taskId: number): Promise<CheckoutResult[]> {
    return this.request<CheckoutResult[]>(
      "POST",
      `/api/tasks/${taskId}/checkout`
    );
  }

  async refreshTaskPRs(taskId: number): Promise<TaskPR[]> {
    return this.request<TaskPR[]>("POST", `/api/tasks/${taskId}/prs/refresh`);
  }

  // --- Runs ---

  async listRuns(opts?: {
    limit?: number;
    taskId?: number;
  }): Promise<Run[]> {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.taskId) params.set("task_id", String(opts.taskId));
    const qs = params.toString();
    return this.request<Run[]>("GET", `/api/runs${qs ? `?${qs}` : ""}`);
  }

  async cancelRun(id: number): Promise<void> {
    await this.request<unknown>("POST", `/api/runs/${id}/cancel`);
  }

  async getRunLog(id: number): Promise<string> {
    const url = `${this.baseUrl}/api/runs/${id}/log`;
    const res = await fetch(url);
    if (!res.ok) {
      return "";
    }
    return res.text();
  }

  async getRunEvents(id: number): Promise<LogEvent[]> {
    return this.request<LogEvent[]>("GET", `/api/runs/${id}/events`);
  }

  async getRunResume(id: number): Promise<RunResume> {
    return this.request<RunResume>("GET", `/api/runs/${id}/resume`);
  }

  // --- Usage ---

  async getUsage(): Promise<UsageSnapshot> {
    return this.request<UsageSnapshot>("GET", "/api/usage");
  }

  async getUsageHistory(hours: number = 5): Promise<UsageSnapshot[]> {
    return this.request<UsageSnapshot[]>(
      "GET",
      `/api/usage/history?hours=${hours}`
    );
  }

  async getLeaderboard(): Promise<LeaderboardData> {
    return this.request<LeaderboardData>("GET", "/api/usage/leaderboard");
  }

  async getCostEfficiency(days: number = 30): Promise<CostEfficiency> {
    return this.request<CostEfficiency>(
      "GET",
      `/api/usage/cost-efficiency?days=${days}`
    );
  }

  async getStreak(): Promise<StreakData> {
    return this.request<StreakData>("GET", "/api/usage/streak");
  }

  async getAchievements(): Promise<AchievementsData> {
    return this.request<AchievementsData>("GET", "/api/usage/achievements");
  }

  // --- Status ---

  async getStatus(): Promise<StatusInfo> {
    return this.request<StatusInfo>("GET", "/api/status");
  }

  // --- Caffeinate ---

  async getCaffeinate(): Promise<CaffeinateStatus> {
    return this.request<CaffeinateStatus>("GET", "/api/caffeinate");
  }

  async toggleCaffeinate(): Promise<{ status: CaffeinateStatus }> {
    return this.request<{ status: CaffeinateStatus }>(
      "POST",
      "/api/caffeinate"
    );
  }

  // --- Accounts ---

  async getAccounts(): Promise<AccountsResponse> {
    return this.request<AccountsResponse>("GET", "/api/accounts");
  }

  async selectAccount(req: SelectAccountRequest): Promise<void> {
    await this.request<unknown>("POST", "/api/accounts/select", req);
  }

  // --- Voice ---

  async voiceStatus(): Promise<{ state: string; message?: string }> {
    return this.request<{ state: string; message?: string }>("GET", "/api/voice/status");
  }

  async voiceInstall(): Promise<{ state: string; message?: string }> {
    return this.request<{ state: string; message?: string }>("POST", "/api/voice/install");
  }

  async transcribeAudio(audioBlob: Blob): Promise<{ text: string }> {
    const url = `${this.baseUrl}/api/voice/transcribe`;
    const formData = new FormData();
    formData.append("audio", audioBlob, "recording.webm");
    const res = await fetch(url, { method: "POST", body: formData });
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new ApiError(res.status, text || res.statusText);
    }
    return res.json() as Promise<{ text: string }>;
  }

  async createVoiceTask(text: string): Promise<Task> {
    return this.request<Task>("POST", "/api/voice/task", { text });
  }

  // --- Models ---

  async getModels(): Promise<ModelInfo[]> {
    return this.request<ModelInfo[]>("GET", "/api/models");
  }

  // --- Config ---

  async getConfig(): Promise<Config> {
    return this.request<Config>("GET", "/api/config");
  }

  async saveConfig(config: Config): Promise<void> {
    await this.request<unknown>("PUT", "/api/config", config);
  }

  // --- Requests ---

  async listRequests(status?: string): Promise<HumanRequest[]> {
    const qs = status ? `?status=${status}` : "";
    return this.request<HumanRequest[]>("GET", `/api/requests${qs}`);
  }

  async getRequest(id: number): Promise<HumanRequest> {
    return this.request<HumanRequest>("GET", `/api/requests/${id}`);
  }

  async respondToRequest(
    id: number,
    body: { body: string; result?: RequestResult }
  ): Promise<Comment> {
    return this.request<Comment>("POST", `/api/requests/${id}/respond`, body);
  }

  async approveRequest(
    id: number,
    scope: string = "once"
  ): Promise<{ status: string }> {
    return this.request<{ status: string }>("POST", `/api/requests/${id}/approve`, { scope });
  }

  async denyRequest(id: number): Promise<{ status: string }> {
    return this.request<{ status: string }>("POST", `/api/requests/${id}/deny`, {});
  }

  // --- Captures ---

  async listCaptures(taskId?: number): Promise<Capture[]> {
    const qs = taskId ? `?task_id=${taskId}` : "";
    return this.request<Capture[]>("GET", `/api/captures${qs}`);
  }

  async getCapture(id: number): Promise<Capture> {
    return this.request<Capture>("GET", `/api/captures/${id}`);
  }

  async createCapture(body: {
    task_id: number;
    request_id?: number;
    initiator: string;
    target_desc: string;
    mode: string;
  }): Promise<Capture> {
    return this.request<Capture>("POST", "/api/captures", body);
  }

  async finishCapture(
    id: number,
    body: { video_path?: string; transcript?: string; duration_sec?: number }
  ): Promise<{ status: string }> {
    return this.request<{ status: string }>(
      "POST",
      `/api/captures/${id}/finish`,
      body
    );
  }

  async setCaptureNotes(
    id: number,
    notes: string
  ): Promise<{ status: string }> {
    return this.request<{ status: string }>(
      "POST",
      `/api/captures/${id}/notes`,
      { notes }
    );
  }

  // --- SSE ---

  connectSSE(): EventSource {
    return new EventSource(`${this.baseUrl}/api/events`);
  }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public body: string
  ) {
    super(`API error ${status}: ${body}`);
    this.name = "ApiError";
  }
}

function detectBaseUrl(): string {
  if (typeof window === "undefined") return "";
  // The desktop shell injects the daemon's actual port via
  // initialization_script in desktop/src-tauri/src/lib.rs. The injection is
  // the authoritative "we are inside the shell" signal: origin sniffing broke
  // under `cargo tauri dev`, whose built-in server hosts the UI at plain
  // http://localhost:<port>, not tauri://localhost — the shell still injects,
  // but the origin check said "browser" and every API call went to the static
  // server.
  const injected = (window as { __BURNRATE_PORT__?: unknown }).__BURNRATE_PORT__;
  if (typeof injected === "number" && injected > 0) {
    return `http://127.0.0.1:${injected}`;
  }
  // Older shells inject nothing — fall back to the origin heuristic + default
  // port so they keep working.
  const isTauri =
    window.location.hostname === "tauri.localhost" ||
    window.location.protocol === "tauri:";
  if (isTauri) return "http://127.0.0.1:9112";
  return "";
}

export const baseUrl = detectBaseUrl();
export const client = new BurnrateClient(baseUrl);

export const apiReady: Promise<void> = baseUrl
  ? new Promise((resolve) => {
      let attempts = 0;
      console.log(`[burnrate] waiting for sidecar at ${baseUrl}/health`);
      const poll = () => {
        fetch(`${baseUrl}/health`)
          .then((r) => {
            if (r.ok) {
              console.log(`[burnrate] sidecar ready after ${attempts} attempts`);
              return resolve();
            }
            throw new Error(`status ${r.status}`);
          })
          .catch((err) => {
            attempts++;
            if (attempts <= 5 || attempts % 10 === 0) {
              console.log(`[burnrate] health poll #${attempts}: ${err}`);
            }
            setTimeout(poll, attempts < 10 ? 500 : 2000);
          });
      };
      poll();
    })
  : Promise.resolve();
