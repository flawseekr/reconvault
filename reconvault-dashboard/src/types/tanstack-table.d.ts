import "@tanstack/react-table";

declare module "@tanstack/react-table" {
  // Presentation hints attached to column definitions.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    className?: string;
    align?: "left" | "right";
  }

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface TableMeta<TData extends RowData> {
    /** "path" shows only path and query in the Endpoint column (host page). */
    endpointDisplay?: "url" | "path";
  }
}
