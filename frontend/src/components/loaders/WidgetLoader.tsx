import PageLoader from "@/components/loaders/PageLoader";

interface WidgetLoaderProps {
  minHeight?: number | string;
  /** What is loading, read out by screen readers. */
  label?: string;
}

export default function WidgetLoader({
  minHeight = 180,
  label,
}: WidgetLoaderProps) {
  return (
    <div
      style={{
        alignItems: "center",
        display: "flex",
        justifyContent: "center",
        minHeight,
        width: "100%",
      }}
    >
      <PageLoader label={label} />
    </div>
  );
}
