import React from "react";

import "./app-table.css";

/* ── Container ──────────────────────────────── */

export const AppTableContainer = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    className={`app-table-container custom-scrollbar ${className || ""}`.trim()}
    ref={ref}
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
    className={`app-table ${className || ""}`.trim()}
    ref={ref}
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
    className={`app-table-head ${className || ""}`.trim()}
    ref={ref}
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
    className={`app-table-body ${className || ""}`.trim()}
    ref={ref}
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
    className={`app-table-row${selected ? " app-table-row--selected" : ""} ${className || ""}`.trim()}
    ref={ref}
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
    className={`app-table-cell ${className || ""}`.trim()}
    ref={ref}
    style={{ textAlign: align, ...style }}
    {...props}
  />
));
AppTableCell.displayName = "AppTableCell";
