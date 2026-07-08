import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { api, type HelloResponse, type MeResponse } from "../api/client";

// ---------------------------------------------------------------------------
// CurrentUser context — available to any component inside Layout
// ---------------------------------------------------------------------------

interface CurrentUser {
  userName: string;
  isAdmin: boolean;
  isLocalAccess: boolean;
  /** true while the /api/me request is in flight */
  loading: boolean;
}

const defaultCurrentUser: CurrentUser = {
  userName: "",
  isAdmin: false,
  isLocalAccess: false,
  loading: true,
};

const CurrentUserContext = createContext<CurrentUser>(defaultCurrentUser);

/** Read the logged-in user's identity and admin status from anywhere inside Layout. */
export function useCurrentUser(): CurrentUser {
  return useContext(CurrentUserContext);
}

// ---------------------------------------------------------------------------
// Connection-status hook (used for the header badge)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

export default function Layout({ children }: { children: ReactNode }) {
  const status = useConnectionStatus();
  const [showPopover, setShowPopover] = useState(false);
  const badgeRef = useRef<HTMLDivElement>(null);

  const [currentUser, setCurrentUser] = useState<CurrentUser>(defaultCurrentUser);

  useEffect(() => {
    api
      .me()
      .then((me: MeResponse) => {
        setCurrentUser({
          userName: me.user_name,
          isAdmin: me.is_admin,
          isLocalAccess: me.is_local_access,
          loading: false,
        });
      })
      .catch(() => {
        setCurrentUser({ ...defaultCurrentUser, loading: false });
      });
  }, []);

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
    <CurrentUserContext.Provider value={currentUser}>
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
          {currentUser.loading ? (
            <div className="text-xs text-gray-400 py-4">Loading...</div>
          ) : !currentUser.isAdmin ? (
            <AccessRestricted
              userName={currentUser.userName}
              yourIP={status?.your_ip ?? ""}
            />
          ) : (
            children
          )}
        </main>
      </div>
    </CurrentUserContext.Provider>
  );
}

// ---------------------------------------------------------------------------
// AccessRestricted — shown to non-admin tailnet users
// ---------------------------------------------------------------------------

function AccessRestricted({ userName, yourIP }: { userName: string; yourIP: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-20 text-center">
      <div className="w-10 h-10 rounded-full bg-gray-100 flex items-center justify-center mb-4">
        <svg
          className="w-5 h-5 text-gray-400"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          viewBox="0 0 24 24"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M16.5 10.5V6.75a4.5 4.5 0 1 0-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 0 0 2.25-2.25v-6.75a2.25 2.25 0 0 0-2.25-2.25H6.75a2.25 2.25 0 0 0-2.25 2.25v6.75a2.25 2.25 0 0 0 2.25 2.25Z"
          />
        </svg>
      </div>
      <h2 className="text-sm font-medium text-gray-700 mb-1">Access restricted</h2>
      <p className="text-xs text-gray-400 max-w-xs leading-relaxed">
        Contact your HeadFwd admin to gain access to the management portal.
      </p>
      {(userName || yourIP) && (
        <div className="mt-5 border border-gray-200 rounded bg-white px-4 py-3 text-xs space-y-1.5 w-56 text-left">
          {yourIP && <PopoverRow label="IP" value={yourIP} mono />}
          {userName
            ? <PopoverRow label="User" value={userName} />
            : <PopoverRow label="User" value="—" muted />
          }
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// PopoverRow
// ---------------------------------------------------------------------------

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
