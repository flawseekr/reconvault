import { MenuIcon, ShieldIcon } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";

import { ApiKeyDialog } from "@/components/common/ApiKeyDialog";
import { Sidebar } from "@/components/layout/Sidebar";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { useApiKey } from "@/hooks/use-api-key";

export function AppLayout() {
  const apiKey = useApiKey();
  const [navOpen, setNavOpen] = useState(false);
  const [changingKey, setChangingKey] = useState(false);

  const openKeyDialog = () => {
    setNavOpen(false);
    setChangingKey(true);
  };

  return (
    <div className="min-h-dvh">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      >
        Skip to content
      </a>

      <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 border-r border-sidebar-border bg-sidebar lg:block">
        <Sidebar onChangeApiKey={openKeyDialog} />
      </aside>

      <Sheet open={navOpen} onOpenChange={setNavOpen}>
        <SheetContent>
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <SheetDescription className="sr-only">Main navigation</SheetDescription>
          <Sidebar onNavigate={() => setNavOpen(false)} onChangeApiKey={openKeyDialog} />
        </SheetContent>
      </Sheet>

      <div className="lg:pl-60">
        <div className="sticky top-0 z-20 flex h-12 items-center gap-2 border-b border-border bg-background/85 px-3 backdrop-blur lg:hidden">
          <Button variant="ghost" size="icon-sm" onClick={() => setNavOpen(true)} aria-label="Open navigation">
            <MenuIcon />
          </Button>
          <ShieldIcon className="size-4 text-primary" aria-hidden="true" />
          <span className="text-sm font-semibold">ReconVault</span>
        </div>

        <main id="main" className="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
          <Outlet />
        </main>
      </div>

      {/* A key is required for every /api/v1 call; the dialog blocks until one is set. */}
      <ApiKeyDialog open={apiKey === null} />
      <ApiKeyDialog open={apiKey !== null && changingKey} onOpenChange={setChangingKey} />
    </div>
  );
}
