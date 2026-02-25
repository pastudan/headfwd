import { type ReactNode, useEffect, useRef, useState } from "react";
import { api, type HelloResponse } from "../api/client";

function useConnectionStatus() {
  const [data, setData] = useState<HelloResponse | null>(null);
  useEffect(() => {
    api.hello().then(setData).catch(() => {});
  }, []);
  return data;
}

function classify(data: HelloResponse) {
  const ip = data.your_ip;
  const onTailnet = ip.startsWith("100.") || ip.startsWith("fd7a:") || data.is_tailnet;
  return { onTailnet };
}

export default function Layout({ children }: { children: ReactNode }) {
  const status = useConnectionStatus();
  const [showPopover, setShowPopover] = useState(false);
  const badgeRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!showPopover) return;
    const handler = (e: MouseEvent) => {
      if (badgeRef.current && !badgeRef.current.contains(e.target as Node)) {
        setShowPopover(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [showPopover]);

  const classified = status ? classify(status) : null;

  return (
    <div className="min-h-screen flex flex-col bg-gray-50">
      <header className="bg-white border-b border-gray-200 sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-4 h-10 flex items-center gap-3">
          <span className="text-sm font-medium text-gray-700 tracking-tight">HeadFwd</span>
          <div className="flex-1" />

          {classified && (
            <div className="relative" ref={badgeRef}>
              <button
                onClick={() => setShowPopover((v) => !v)}
                className={`flex items-center gap-1.5 text-xs px-2.5 py-1 rounded border transition-colors select-none ${
                  classified.onTailnet
                    ? "border-green-200 text-green-700 bg-green-50 hover:bg-green-100"
                    : "border-amber-200 text-amber-600 bg-amber-50 hover:bg-amber-100"
                }`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                    classified.onTailnet ? "bg-green-500" : "bg-amber-400"
                  }`}
                />
                {classified.onTailnet ? "On tailnet" : "Not on tailnet"}
              </button>

              {showPopover && (
                <div className="absolute right-0 top-full mt-2 w-60 bg-white border border-gray-200 rounded-lg shadow-lg z-50 overflow-hidden">
                  <div
                    className={`px-3 py-2 border-b text-xs font-medium ${
                      classified.onTailnet
                        ? "bg-green-50 border-green-100 text-green-700"
                        : "bg-amber-50 border-amber-100 text-amber-600"
                    }`}
                  >
                    {classified.onTailnet
                      ? "Accessing via Headscale mesh"
                      : "Not connected via tailnet"}
                  </div>
                  <div className="px-3 py-2.5 space-y-1.5">
                    <PopoverRow label="IP" value={status!.your_ip} mono />
                    {status!.user_name
                      ? <PopoverRow label="User" value={status!.user_name} />
                      : <PopoverRow label="User" value="—" muted />
                    }
                    {status!.node_name && (
                      <PopoverRow label="Device" value={status!.node_name} mono />
                    )}
                  </div>
                  {!classified.onTailnet && (
                    <p className="px-3 pb-2.5 text-xs text-gray-400 leading-relaxed">
                      Connect via the mesh to see your identity.
                    </p>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </header>
      <main className="flex-1 max-w-5xl mx-auto px-4 py-6 w-full">
        {children}
      </main>
    </div>
  );
}

function PopoverRow({
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
    <div className="flex items-center gap-2 text-xs">
      <span className="text-gray-400 w-14 shrink-0">{label}</span>
      <span
        className={`${mono ? "font-mono" : ""} ${
          muted ? "text-gray-300 italic" : "text-gray-700"
        } truncate`}
      >
        {value}
      </span>
    </div>
  );
}
