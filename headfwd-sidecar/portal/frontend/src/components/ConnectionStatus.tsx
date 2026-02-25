import { useEffect, useState } from "react";
import { api, type HelloResponse } from "../api/client";

function classify(data: HelloResponse): {
  color: "green" | "orange";
  label: string;
  detail: string;
} {
  const ip = data.your_ip;

  if (ip.startsWith("100.") || ip.startsWith("fd7a:") || data.is_tailnet) {
    return {
      color: "green",
      label: "On tailnet",
      detail: ip,
    };
  }

  return {
    color: "orange",
    label: "Not on tailnet",
    detail: ip,
  };
}

export default function ConnectionStatus() {
  const [data, setData] = useState<HelloResponse | null>(null);

  useEffect(() => {
    api
      .hello()
      .then(setData)
      .catch(() => {});
  }, []);

  if (!data) return null;

  const { color, label } = classify(data);

  const isLocal =
    data.your_ip === "::1" ||
    data.your_ip.startsWith("127.") ||
    data.your_ip.startsWith("192.168.") ||
    data.your_ip.startsWith("10.") ||
    data.your_ip.startsWith("172.");

  return (
    <div className="border border-gray-200 rounded bg-white overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-gray-100">
        <span
          className={`w-2 h-2 rounded-full shrink-0 ${
            color === "green" ? "bg-green-500" : "bg-amber-400"
          }`}
        />
        <span
          className={`text-xs font-medium ${
            color === "green" ? "text-green-700" : "text-amber-600"
          }`}
        >
          {label}
        </span>
      </div>

      <div className="px-3 py-2 space-y-1.5 text-xs">
        <Row label="IP address" value={data.your_ip} mono />
        {data.user_name ? (
          <Row label="User" value={data.user_name} />
        ) : (
          <Row label="User" value={isLocal ? "—" : "Unknown"} muted />
        )}
        {data.node_name && <Row label="Device" value={data.node_name} mono />}
      </div>

      <p className="px-3 pb-2 text-xs text-gray-400 leading-relaxed">
        {color === "green"
          ? "You are accessing this portal over the Headscale mesh network."
          : isLocal
            ? "You are accessing this portal directly via localhost or a local network address. Headscale user identity is not available."
            : "Your IP address is not recognised as a Headscale address. Connect via the mesh to see your identity."}
      </p>
    </div>
  );
}

function Row({
  label,
  value,
  mono = false,
  muted = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
  muted?: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-gray-400 w-20 shrink-0">{label}</span>
      <span
        className={`${mono ? "font-mono" : ""} ${
          muted ? "text-gray-300 italic" : "text-gray-700"
        }`}
      >
        {value}
      </span>
    </div>
  );
}
