const BASE = "";

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text}`);
  }
  if (res.status === 204 || res.headers.get("content-length") === "0") {
    return undefined as T;
  }
  return res.json();
}

export interface User {
  name: string;
  id?: string;
  createdAt?: string;
}

export interface Node {
  id: string;
  givenName: string;
  name: string;
  ipAddresses: string[];
  online: boolean;
  lastSeen?: string;
  createdAt?: string;
  forcedTags: string[];
  validTags: string[];
  user: User;
}

export interface HelloResponse {
  message: string;
  your_ip: string;
  is_tailnet: boolean;
  timestamp: string;
  node_name?: string;
  user_name?: string;
}

export interface MeResponse {
  user_name: string;
  is_admin: boolean;
  is_local_access: boolean;
}

export interface CreateKeyResponse {
  key: string;
  user: string;
  qr_data: string;
  expires_at: string;
}

export const api = {
  hello: () => request<HelloResponse>("/api/hello"),

  listUsers: () =>
    request<{ users: User[] }>("/api/users").then((r) => r.users ?? []),

  createUser: (name: string) =>
    request<{ user: User }>("/api/users", {
      method: "POST",
      body: JSON.stringify({ name }),
    }).then((r) => r.user),

  listNodes: () =>
    request<{ nodes: Node[] }>("/api/nodes").then((r) => r.nodes ?? []),

  createKey: (user: string, reusable = false) =>
    request<CreateKeyResponse>("/api/keys", {
      method: "POST",
      body: JSON.stringify({ user, reusable }),
    }),

  deleteUser: (name: string) =>
    request<void>(`/api/users/${encodeURIComponent(name)}`, { method: "DELETE" }),

  deleteNode: (id: string) =>
    request<void>(`/api/nodes/${encodeURIComponent(id)}`, { method: "DELETE" }),

  me: () => request<MeResponse>("/api/me"),

  listAdmins: () =>
    request<{ admins: string[] }>("/api/admins").then((r) => r.admins ?? []),

  grantAdmin: (name: string) =>
    request<void>(`/api/admins/${encodeURIComponent(name)}`, { method: "PUT" }),

  revokeAdmin: (name: string) =>
    request<void>(`/api/admins/${encodeURIComponent(name)}`, { method: "DELETE" }),
};
