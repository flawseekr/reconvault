import { CompassIcon } from "lucide-react";
import { Link } from "react-router";

import { EmptyState } from "@/components/common/EmptyState";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { routes } from "@/lib/routes";

export function NotFoundPage() {
  return (
    <>
      <PageHeader title="Page not found" breadcrumbs={[{ label: "Home", to: routes.home }, { label: "Not found" }]} />
      <EmptyState
        icon={CompassIcon}
        title="No page at this address"
        action={
          <Button asChild variant="outline" size="sm">
            <Link to={routes.home}>Back to Home</Link>
          </Button>
        }
      />
    </>
  );
}
