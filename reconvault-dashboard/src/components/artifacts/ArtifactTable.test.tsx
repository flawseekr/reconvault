import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { type ArtifactSort, ArtifactTable } from "@/components/artifacts/ArtifactTable";
import { TooltipProvider } from "@/components/ui/tooltip";
import { summary } from "@/test/fixtures";
import type { ArtifactSummary } from "@/types/api";

interface SetupOptions {
  sort?: ArtifactSort;
  sortMode?: "server" | "client";
}

function setup(data: ArtifactSummary[], { sort = { sortBy: "requested_at", sortOrder: "DESC" }, sortMode = "server" }: SetupOptions = {}) {
  const onSortChange = vi.fn();
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: (
          <ArtifactTable data={data} sort={sort} onSortChange={onSortChange} sortMode={sortMode} caption="Artifacts" empty={<p>Nothing here</p>} />
        ),
      },
      { path: "/http/:id", element: <p>detail page</p> },
    ],
    { initialEntries: ["/"] },
  );
  render(
    <TooltipProvider>
      <RouterProvider router={router} />
    </TooltipProvider>,
  );
  return { onSortChange, router };
}

/** Accessible names of the endpoint links, in row order. */
const rowsText = () =>
  screen
    .getAllByRole("row")
    .slice(1)
    .map((r) => within(r).getByRole("link").getAttribute("aria-label"));

describe("ArtifactTable", () => {
  it("renders the endpoint and formatted fields, with fallbacks for missing values", () => {
    setup([
      summary(),
      summary({ id: "650e8400-e29b-41d4-a716-446655440000", http_method: "POST", full_url: "https://failed.test/", response_status_code: null, response_content_type: null, response_body_size_bytes: null }),
    ]);
    const [, ok, failed] = screen.getAllByRole("row");
    expect(within(ok!).getByRole("link", { name: "GET https://api.example.com/v1/resource" })).toBeInTheDocument();
    expect(within(ok!).getByText("GET")).toBeInTheDocument();
    expect(within(ok!).getByText("200")).toHaveAttribute("data-status-class", "2xx");
    expect(within(ok!).getByText("1.5 KB")).toBeInTheDocument();
    expect(within(failed!).getByLabelText("No response")).toHaveTextContent("—");
    expect(within(failed!).getAllByText("—").length).toBeGreaterThanOrEqual(3);
  });

  it("marks the active sort column and toggles direction", async () => {
    const user = userEvent.setup();
    const { onSortChange } = setup([summary()], { sort: { sortBy: "response_status_code", sortOrder: "ASC" } });
    expect(screen.getByRole("columnheader", { name: /status/i })).toHaveAttribute("aria-sort", "ascending");
    expect(screen.getByRole("columnheader", { name: /endpoint/i })).toHaveAttribute("aria-sort", "none");

    await user.click(screen.getByRole("button", { name: /^status/i }));
    expect(onSortChange).toHaveBeenLastCalledWith({ sortBy: "response_status_code", sortOrder: "DESC" });
    await user.click(screen.getByRole("button", { name: /^size/i }));
    expect(onSortChange).toHaveBeenLastCalledWith({ sortBy: "response_body_size_bytes", sortOrder: "ASC" });
  });

  it("navigates to the artifact on row click but not on modified clicks", async () => {
    const user = userEvent.setup();
    const { router } = setup([summary()]);
    const row = screen.getAllByRole("row")[1]!;
    const statusCell = within(row).getByText("200");

    await user.keyboard("{Control>}");
    await user.click(statusCell);
    await user.keyboard("{/Control}");
    expect(router.state.location.pathname).toBe("/");

    await user.click(statusCell);
    expect(router.state.location.pathname).toBe("/http/550e8400-e29b-41d4-a716-446655440000");
  });

  it("exposes each row as a keyboard-reachable link with the full URL", () => {
    const long = `https://api.example.com/${"segment/".repeat(30)}end`;
    setup([summary({ full_url: long })]);
    const link = screen.getByRole("link", { name: `GET ${long}` });
    expect(link.textContent).toContain("…"); // visibly truncated
  });

  it("links rows to their detail page", () => {
    setup([summary()]);
    expect(screen.getByRole("link", { name: /GET https:\/\/api\.example\.com/ })).toHaveAttribute(
      "href",
      "/http/550e8400-e29b-41d4-a716-446655440000",
    );
  });

  it("sorts loaded rows in client mode with missing values last", () => {
    const data = [
      summary({ id: "a0000000-0000-0000-0000-000000000000", full_url: "https://a/", response_status_code: 500 }),
      summary({ id: "b0000000-0000-0000-0000-000000000000", full_url: "https://b/", response_status_code: null }),
      summary({ id: "c0000000-0000-0000-0000-000000000000", full_url: "https://c/", response_status_code: 200 }),
    ];
    setup(data, { sortMode: "client", sort: { sortBy: "response_status_code", sortOrder: "ASC" } });
    expect(rowsText()).toEqual(["GET https://c/", "GET https://a/", "GET https://b/"]);
  });

  it("keeps server order in server mode", () => {
    const data = [summary({ id: "a0000000-0000-0000-0000-000000000000", full_url: "https://z/" }), summary({ id: "b0000000-0000-0000-0000-000000000000", full_url: "https://a/" })];
    setup(data, { sort: { sortBy: "full_url", sortOrder: "ASC" } });
    expect(rowsText()).toEqual(["GET https://z/", "GET https://a/"]);
  });

  it("renders the empty state", () => {
    setup([]);
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
  });
});
