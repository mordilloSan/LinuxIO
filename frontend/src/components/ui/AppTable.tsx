import React from "react";

import "./app-table.css";

/* ── Container ──────────────────────────────── */

export const AppTableContainer = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={`app-table-container custom-scrollbar ${className || ""}`.trim()}
    {...props}
  />
));
AppTableContainer.displayName = "AppTableContainer";

/* ── Table ──────────────────────────────────── */

export const AppTable = React.forwardRef<
  HTMLTableElement,
  React.TableHTMLAttributes<HTMLTableElement>
>(({ className, ...props }, ref) => (
  <table
    ref={ref}
    className={`app-table ${className || ""}`.trim()}
    {...props}
  />
));
AppTable.displayName = "AppTable";

/* ── Head ───────────────────────────────────── */

export const AppTableHead = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <thead
    ref={ref}
    className={`app-table-head ${className || ""}`.trim()}
    {...props}
  />
));
AppTableHead.displayName = "AppTableHead";

/* ── Body ───────────────────────────────────── */

export const AppTableBody = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tbody
    ref={ref}
    className={`app-table-body ${className || ""}`.trim()}
    {...props}
  />
));
AppTableBody.displayName = "AppTableBody";

/* ── Row ────────────────────────────────────── */

interface AppTableRowProps extends React.HTMLAttributes<HTMLTableRowElement> {
  selected?: boolean;
}

export const AppTableRow = React.forwardRef<
  HTMLTableRowElement,
  AppTableRowProps
>(({ className, selected, ...props }, ref) => (
  <tr
    ref={ref}
    className={`app-table-row${selected ? " app-table-row--selected" : ""} ${className || ""}`.trim()}
    {...props}
  />
));
AppTableRow.displayName = "AppTableRow";

/* ── Cell ───────────────────────────────────── */

interface AppTableCellProps extends React.TdHTMLAttributes<HTMLTableCellElement> {
  align?: "left" | "center" | "right";
  component?: "td" | "th";
}

export const AppTableCell = React.forwardRef<
  HTMLTableCellElement,
  AppTableCellProps
>(({ align, component: Component = "td", className, style, ...props }, ref) => (
  <Component
    ref={ref}
    className={`app-table-cell ${className || ""}`.trim()}
    style={{ textAlign: align, ...style }}
    {...props}
  />
));
AppTableCell.displayName = "AppTableCell";
