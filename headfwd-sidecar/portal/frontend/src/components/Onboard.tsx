import { useEffect, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { api, type User, type CreateKeyResponse } from "../api/client";

export default function Onboard() {
  const [users, setUsers] = useState<User[]>([]);
  const [selectedUser, setSelectedUser] = useState("");
  const [keyData, setKeyData] = useState<CreateKeyResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.listUsers().then(setUsers).catch(() => {});
  }, []);

  const handleGenerate = async () => {
    if (!selectedUser) return;
    setLoading(true);
    setError(null);
    setKeyData(null);
    try {
      const data = await api.createKey(selectedUser);
      setKeyData(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate key");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">Onboard Device</h2>
        <p className="text-gray-400 mt-1">
          Generate a preauth key and QR code to connect a new device to the
          mesh.
        </p>
      </div>

      <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 space-y-4">
        <div>
          <label className="block text-sm text-gray-400 mb-2">
            Select user
          </label>
          <select
            value={selectedUser}
            onChange={(e) => {
              setSelectedUser(e.target.value);
              setKeyData(null);
            }}
            className="w-full bg-gray-950 border border-gray-700 rounded-lg px-4 py-2 text-sm text-gray-200 focus:outline-none focus:border-emerald-600"
          >
            <option value="">Choose a user...</option>
            {users.map((u) => (
              <option key={u.name} value={u.name}>
                {u.name}
              </option>
            ))}
          </select>
        </div>

        <button
          onClick={handleGenerate}
          disabled={loading || !selectedUser}
          className="w-full px-5 py-3 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 rounded-lg font-medium transition-colors"
        >
          {loading ? "Generating..." : "Generate Preauth Key"}
        </button>
      </div>

      {error && (
        <div className="bg-red-950/50 border border-red-800 rounded-xl p-4 text-red-400 text-sm">
          {error}
        </div>
      )}

      {keyData && (
        <div className="bg-gray-900 border border-gray-800 rounded-xl p-6 space-y-6">
          <div className="flex justify-center">
            <div className="bg-white p-4 rounded-xl">
              <QRCodeSVG value={keyData.qr_data} size={220} level="M" />
            </div>
          </div>

          <p className="text-center text-gray-400 text-sm">
            Scan this QR code with the HeadFwd iOS app to join the mesh.
          </p>

          <div className="space-y-3">
            <Field label="Preauth Key" value={keyData.key} mono />
            <Field label="User" value={keyData.user} />
            <Field label="Expires" value={keyData.expires_at} />
            <Field label="QR Data" value={keyData.qr_data} mono />
          </div>
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="bg-gray-950 rounded-lg px-4 py-3 flex items-start justify-between gap-4">
      <span className="text-xs text-gray-500 uppercase tracking-wide shrink-0 pt-0.5">
        {label}
      </span>
      <span
        className={`text-sm text-gray-300 text-right break-all ${mono ? "font-mono" : ""}`}
      >
        {value}
      </span>
    </div>
  );
}
