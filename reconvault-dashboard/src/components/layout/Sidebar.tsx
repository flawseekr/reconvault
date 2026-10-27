import { HomeIcon, KeyRoundIcon, type LucideIcon, SearchIcon, ShieldIcon } from "lucide-react";
import { NavLink } from "react-router";

import { Button } from "@/components/ui/button";
import { useApiHealth } from "@/features/health";
import { cn } from "@/lib/utils";

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  end?: boolean;
}

const NAV: NavItem[] = [
  { to: "/", label: "Home", icon: HomeIcon, end: true },
  { to: "/search", label: "Search", icon: SearchIcon },
];

interface SidebarProps {
  onNavigate?: () => void;
  onChangeApiKey: () => void;
}

export function Sidebar({ onNavigate, onChangeApiKey }: SidebarProps) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-2.5 border-b border-sidebar-border px-4">
        <div className="flex size-7 items-center justify-center rounded-md border border-primary/30 bg-primary/10">
          <ShieldIcon className="size-4 text-primary" aria-hidden="true" />
        </div>
        <div className="leading-tight">
          <p className="text-sm font-semibold tracking-tight">ReconVault</p>
          <p className="text-[11px] text-muted-foreground">HTTP artifact explorer</p>
        </div>
      </div>

      <nav aria-label="Main" className="flex-1 px-2 py-3">
        <ul className="grid gap-0.5">
          {NAV.map(({ to, label, icon: Icon, end }) => (
            <li key={to}>
              <NavLink
                to={to}
                end={end}
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn(
                    "flex h-8 items-center gap-2.5 rounded-md px-2.5 text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
                    isActive
                      ? "bg-primary/12 font-medium text-primary"
                      : "text-muted-foreground hover:bg-accent hover:text-foreground",
                  )
                }
              >
                <Icon className="size-4" aria-hidden="true" />
                {label}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>

      <div className="grid gap-1 border-t border-sidebar-border p-2">
        <ApiStatus />
        <Button variant="ghost" size="sm" className="justify-start gap-2.5 px-2.5 text-muted-foreground" onClick={onChangeApiKey}>
          <KeyRoundIcon /> Change API key
        </Button>
      </div>
    </div>
  );
}

function ApiStatus() {
  const { isSuccess, isError, isPending } = useApiHealth();
  const state = isPending ? "checking" : isSuccess ? "online" : isError ? "unreachable" : "checking";
  return (
    <div className="flex h-8 items-center gap-2.5 px-2.5 text-xs text-muted-foreground" role="status">
      <span
        className={cn(
          "size-2 rounded-full",
          state === "online" && "bg-status-2xx shadow-[0_0_0_3px] shadow-status-2xx/15",
          state === "unreachable" && "bg-status-5xx shadow-[0_0_0_3px] shadow-status-5xx/15",
          state === "checking" && "animate-pulse bg-status-none",
        )}
        aria-hidden="true"
      />
      API {state}
    </div>
  );
}
