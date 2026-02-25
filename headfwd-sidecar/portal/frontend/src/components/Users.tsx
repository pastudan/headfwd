import { useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { api, type CreateKeyResponse, type Node, type User } from "../api/client";

export default function Users() {
  const [users, setUsers] = useState<User[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [onboardingUser, setOnboardingUser] = useState<string | null>(null);
  const [keyData, setKeyData] = useState<Record<string, CreateKeyResponse>>({});
  const [generatingKey, setGeneratingKey] = useState<string | null>(null);

  const refresh = async () => {
    try {
      const [u, n] = await Promise.all([api.listUsers(), api.listNodes()]);
      setUsers(u);
      setNodes(n);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { refresh(); }, []);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    const name = newName.trim();
    if (!name) return;
    setCreating(true);
    try {
      await api.createUser(name);
      setNewName("");
      await refresh();
      handleGenerateKey(name);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create user");
    } finally {
      setCreating(false);
    }
  };

  const handleGenerateKey = async (userName: string) => {
    setOnboardingUser(userName);
    setKeyData((prev) => { const next = { ...prev }; delete next[userName]; return next; });
    setGeneratingKey(userName);
    try {
      const k = await api.createKey(userName);
      setKeyData((prev) => ({ ...prev, [userName]: k }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate key");
    } finally {
      setGeneratingKey(null);
    }
  };

  const nodesForUser = (name: string) => nodes.filter((n) => n.user?.name === name);
  const taggedNodes = nodes.filter((n) => (n.forcedTags?.length ?? 0) > 0);

  if (loading) return <div className="text-xs text-gray-400 py-4">Loading...</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-medium text-gray-900">Devices</h2>
          <p className="text-xs text-gray-400 mt-0.5">
            {users.length} user{users.length !== 1 ? "s" : ""} &middot; {nodes.length} device{nodes.length !== 1 ? "s" : ""}
          </p>
        </div>
        <form onSubmit={handleCreateUser} className="flex gap-1.5 items-center">
          <input
            type="text"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="New user..."
            className="border border-gray-200 rounded px-2.5 py-1 text-xs text-gray-700 placeholder-gray-300 focus:outline-none focus:border-blue-400 w-36 bg-white"
          />
          <button
            type="submit"
            disabled={creating || !newName.trim()}
            className="px-2.5 py-1 bg-blue-600 text-white rounded text-xs hover:bg-blue-700 disabled:opacity-40 transition-colors"
          >
            {creating ? "Adding..." : "Add user"}
          </button>
        </form>
      </div>

      {error && (
        <div className="border border-red-200 rounded bg-red-50 px-3 py-2 text-xs text-red-600">
          {error}
        </div>
      )}

      {users.length === 0 ? (
        <div className="border border-gray-200 rounded bg-white px-3 py-6 text-xs text-gray-400 text-center">
          No users yet.
        </div>
      ) : (
        <div className="border border-gray-200 rounded bg-white divide-y divide-gray-100">
          {users.map((user) => (
            <UserRow
              key={user.name}
              user={user}
              nodes={nodesForUser(user.name)}
              onboardingOpen={onboardingUser === user.name}
              onToggleOnboarding={() => {
                if (onboardingUser === user.name) {
                  setOnboardingUser(null);
                } else {
                  handleGenerateKey(user.name);
                }
              }}
              keyData={keyData[user.name] ?? null}
              generatingKey={generatingKey === user.name}
              onRegenerateKey={() => handleGenerateKey(user.name)}
              onCancel={() => setOnboardingUser(null)}
              onDeviceRegistered={() => { setOnboardingUser(null); refresh(); }}
              onDeleted={refresh}
              onNodeDeleted={refresh}
            />
          ))}
        </div>
      )}

      {taggedNodes.length > 0 && (
        <div className="space-y-1">
          <p className="text-xs text-gray-400 uppercase tracking-wide">Tagged devices</p>
          <div className="border border-gray-200 rounded bg-white divide-y divide-gray-100">
            {taggedNodes.map((node) => (
              <NodeRow key={node.id} node={node} showUser onDeleted={refresh} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// UserRow
// ---------------------------------------------------------------------------

function UserRow({
  user,
  nodes,
  onboardingOpen,
  onToggleOnboarding,
  keyData,
  generatingKey,
  onRegenerateKey,
  onCancel,
  onDeviceRegistered,
  onDeleted,
  onNodeDeleted,
}: {
  user: User;
  nodes: Node[];
  onboardingOpen: boolean;
  onToggleOnboarding: () => void;
  keyData: CreateKeyResponse | null;
  generatingKey: boolean;
  onRegenerateKey: () => void;
  onCancel: () => void;
  onDeviceRegistered: () => void;
  onDeleted: () => void;
  onNodeDeleted: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDelete = async () => {
    setDeleting(true);
    setDeleteError(null);
    try {
      await api.deleteUser(user.name);
      onDeleted();
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : "Delete failed");
      setDeleting(false);
      setConfirmDelete(false);
    }
  };

  return (
    <div>
      {/* User header row */}
      <div className="flex items-center px-3 py-2 gap-3 hover:bg-gray-50 group">
        <span className="text-xs font-medium text-gray-700 w-40 truncate">{user.name}</span>
        <span className="flex-1" />

        {/* Delete — shown on hover or while confirming, sits left of the Add button */}
        {!onboardingOpen && (
          <div className={`flex items-center gap-2 transition-opacity ${confirmDelete ? "opacity-100" : "opacity-0 group-hover:opacity-100"}`}>
            {confirmDelete ? (
              <>
                <span className="text-xs text-red-500">
                  {nodes.length > 0
                    ? `Delete user + ${nodes.length} device${nodes.length !== 1 ? "s" : ""}?`
                    : "Delete user?"}
                </span>
                <button
                  onClick={handleDelete}
                  disabled={deleting}
                  className="text-xs text-red-600 hover:text-red-800 font-medium underline underline-offset-2 cursor-pointer disabled:opacity-50 transition-colors"
                >
                  {deleting ? "…" : "Yes"}
                </button>
                <button
                  onClick={() => setConfirmDelete(false)}
                  className="text-xs text-gray-400 hover:text-gray-600 cursor-pointer transition-colors"
                >
                  Cancel
                </button>
              </>
            ) : (
              <button
                onClick={() => setConfirmDelete(true)}
                className="text-xs text-red-400 hover:text-red-600 underline underline-offset-2 cursor-pointer transition-colors"
              >
                Delete
              </button>
            )}
          </div>
        )}

        <button
          onClick={onToggleOnboarding}
          className={`text-xs px-2 py-0.5 rounded border transition-colors shrink-0 ${
            onboardingOpen
              ? "border-blue-300 text-blue-600 bg-blue-50"
              : "border-gray-200 text-gray-400 hover:text-gray-600 hover:border-gray-300"
          }`}
        >
          {onboardingOpen ? "Close" : "+ Add device"}
        </button>
      </div>

      {deleteError && (
        <div className="px-3 py-1 text-xs text-red-500 bg-red-50 border-t border-red-100">
          {deleteError}
        </div>
      )}

      {/* Devices under user */}
      {nodes.length > 0 && (
        <div className="border-t border-gray-100 divide-y divide-gray-100">
          {nodes.map((node) => (
            <NodeRow key={node.id} node={node} indent onDeleted={onNodeDeleted} />
          ))}
        </div>
      )}

      {/* Inline onboarding */}
      {onboardingOpen && (
        <OnboardPanel
          keyData={keyData}
          loading={generatingKey}
          onRegenerateKey={onRegenerateKey}
          onCancel={onCancel}
          onDeviceRegistered={onDeviceRegistered}
          userName={user.name}
          currentNodeIds={nodes.map((n) => n.id)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// NodeRow
// ---------------------------------------------------------------------------

function NodeRow({
  node,
  indent = false,
  showUser = false,
  onDeleted,
}: {
  node: Node;
  indent?: boolean;
  showUser?: boolean;
  onDeleted?: () => void;
}) {
  const primaryIP = node.ipAddresses?.find((ip) => ip.startsWith("100.")) ?? node.ipAddresses?.[0];
  const tags = [...(node.forcedTags ?? []), ...(node.validTags ?? [])];
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDelete = async () => {
    setDeleting(true);
    setDeleteError(null);
    try {
      await api.deleteNode(node.id);
      onDeleted?.();
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : "Delete failed");
      setDeleting(false);
      setConfirmDelete(false);
    }
  };

  return (
    <>
      <div className={`flex items-center gap-3 py-1.5 pr-3 hover:bg-gray-50 group ${indent ? "pl-10" : "pl-3"}`}>
        <span
          className={`w-1.5 h-1.5 rounded-full shrink-0 ${node.online ? "bg-green-500" : "bg-gray-300"}`}
          title={node.online ? "Online" : "Offline"}
        />
        <span className="text-xs font-mono text-gray-600 flex-1 truncate">{node.givenName}</span>
        {tags.map((tag) => (
          <span key={tag} className="text-xs text-blue-500 border border-blue-200 rounded px-1.5 py-px bg-blue-50 shrink-0">
            {tag}
          </span>
        ))}
        {showUser && node.user && (
          <span className="text-xs text-gray-400 shrink-0">{node.user.name}</span>
        )}

        {/* Delete — shown on hover or while confirming, sits left of the IP */}
        {onDeleted && (
          <div className={`flex items-center gap-1.5 transition-opacity ${confirmDelete ? "opacity-100" : "opacity-0 group-hover:opacity-100"}`}>
            {confirmDelete ? (
              <>
                <span className="text-xs text-red-500">Remove?</span>
                <button
                  onClick={handleDelete}
                  disabled={deleting}
                  className="text-xs text-red-600 hover:text-red-800 font-medium underline underline-offset-2 cursor-pointer disabled:opacity-50 transition-colors"
                >
                  {deleting ? "…" : "Yes"}
                </button>
                <button
                  onClick={() => setConfirmDelete(false)}
                  className="text-xs text-gray-400 hover:text-gray-600 cursor-pointer transition-colors"
                >
                  No
                </button>
              </>
            ) : (
              <button
                onClick={() => setConfirmDelete(true)}
                className="text-xs text-red-400 hover:text-red-600 underline underline-offset-2 cursor-pointer transition-colors"
              >
                Delete
              </button>
            )}
          </div>
        )}

        {primaryIP && (
          <span className="text-xs font-mono text-gray-400 shrink-0">{primaryIP}</span>
        )}
      </div>

      {deleteError && (
        <div className={`${indent ? "pl-10" : "pl-3"} pr-3 py-1 text-xs text-red-500 bg-red-50 border-t border-red-100`}>
          {deleteError}
        </div>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Countdown hook
// ---------------------------------------------------------------------------

function useCountdown(expiresAt: string | null): { label: string; expired: boolean } {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!expiresAt) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [expiresAt]);

  if (!expiresAt) return { label: "", expired: false };
  const secsLeft = Math.floor((new Date(expiresAt).getTime() - Date.now()) / 1000);
  if (secsLeft <= 0) return { label: "Expired", expired: true };
  const m = Math.floor(secsLeft / 60);
  const s = secsLeft % 60;
  return { label: `${m}m ${s.toString().padStart(2, "0")}s`, expired: false };
}

// ---------------------------------------------------------------------------
// OnboardPanel
// ---------------------------------------------------------------------------

function OnboardPanel({
  keyData,
  loading,
  onRegenerateKey,
  onCancel,
  onDeviceRegistered,
  userName,
  currentNodeIds,
}: {
  keyData: CreateKeyResponse | null;
  loading: boolean;
  onRegenerateKey: () => void;
  onCancel: () => void;
  onDeviceRegistered: () => void;
  userName: string;
  currentNodeIds: string[];
}) {
  const [copied, setCopied] = useState(false);
  const { label: expiryLabel, expired } = useCountdown(keyData?.expires_at ?? null);

  const baselineIds = useRef<Set<string> | null>(null);
  if (baselineIds.current === null) {
    baselineIds.current = new Set(currentNodeIds);
  }

  useEffect(() => {
    if (!keyData || expired) return;
    const interval = setInterval(async () => {
      try {
        const nodes = await api.listNodes();
        const newNode = nodes.find(
          (n) => n.user?.name === userName && !baselineIds.current!.has(n.id)
        );
        if (newNode) onDeviceRegistered();
      } catch { /* ignore transient errors */ }
    }, 3000);
    return () => clearInterval(interval);
  }, [keyData, expired, userName, onDeviceRegistered]);

  const copy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="border-t border-blue-100 bg-blue-50/40 px-4 py-3">
      <p className="text-xs text-gray-400 mb-3">
        New device for <span className="text-gray-600 font-medium">{userName}</span>
      </p>

      {loading && (
        <p className="text-xs text-gray-400">Generating key...</p>
      )}

      {!loading && keyData && (
        <div className="flex gap-5 items-start flex-wrap">
          <div className={`border rounded bg-white p-2 shrink-0 transition-opacity ${expired ? "opacity-30" : "border-gray-200"}`}>
            <QRCodeSVG value={keyData.qr_data} size={128} level="M" />
          </div>
          <div className="flex-1 min-w-0 space-y-1.5 text-xs">
            {expired ? (
              <div className="flex items-center gap-2">
                <span className="text-amber-600">Key expired.</span>
                <button
                  onClick={onRegenerateKey}
                  className="text-amber-600 hover:text-amber-800 underline underline-offset-2 transition-colors font-medium"
                >
                  Regenerate?
                </button>
                <button
                  onClick={onCancel}
                  className="text-gray-400 hover:text-gray-600 underline underline-offset-2 transition-colors"
                >
                  Cancel
                </button>
              </div>
            ) : (
              <>
                <KeyField
                  label="Preauth key"
                  value={keyData.key}
                  onCopy={() => copy(keyData.key)}
                  copied={copied}
                />
                <KeyField
                  label="Expires in"
                  value={expiryLabel}
                  valueClass="tabular-nums"
                />
                <div className="flex items-center gap-3 pt-1">
                  <button
                    onClick={onRegenerateKey}
                    className="text-xs text-gray-400 hover:text-gray-600 underline underline-offset-2 transition-colors"
                  >
                    Regenerate
                  </button>
                  <button
                    onClick={onCancel}
                    className="text-xs text-gray-400 hover:text-gray-600 underline underline-offset-2 transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// KeyField
// ---------------------------------------------------------------------------

function KeyField({
  label,
  value,
  onCopy,
  copied,
  valueClass = "font-mono",
}: {
  label: string;
  value: string;
  onCopy?: () => void;
  copied?: boolean;
  valueClass?: string;
}) {
  return (
    <div className="flex items-center gap-2 bg-white border border-gray-200 rounded px-2.5 py-1.5">
      <span className="text-gray-400 w-16 shrink-0">{label}</span>
      <span className={`${valueClass} text-gray-600 flex-1 truncate text-xs`}>{value}</span>
      {onCopy && (
        <button onClick={onCopy} className="text-gray-400 hover:text-gray-700 shrink-0 transition-colors">
          {copied ? "✓" : "Copy"}
        </button>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// relativeTime
// ---------------------------------------------------------------------------

