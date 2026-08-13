export type DeliveryStatus = "active" | "completed" | "blocked";

export interface Delivery {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: DeliveryStatus;
  current_stage: string;
  pending_gate: string | null;
  fail_count: number;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  repo_url: string;
  default_branch: string;
  created_at: string;
  updated_at: string;
}

// payload 在后端是 jsonb -> []byte，序列化为 base64 字符串；P1 不渲染，标 unknown。
export interface TimelineEvent {
  id: string;
  delivery_id: string;
  stage: string;
  event_type: string;
  payload: unknown;
  created_at: string;
}

export interface DeliveryDetail {
  delivery: Delivery;
  timeline: TimelineEvent[];
}

export const STAGES = [
  "intake",
  "spec",
  "spec_approval",
  "test_gen",
  "code_gen",
  "unit_test",
  "code_review",
] as const;

export const GATES = new Set(["spec_approval", "code_review"]);
