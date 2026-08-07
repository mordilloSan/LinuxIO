import PageLoader from "@/components/loaders/PageLoader";

interface WidgetLoaderProps {
  minHeight?: number | string;
}

export default function WidgetLoader({ minHeight = 180 }: WidgetLoaderProps) {
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
      <PageLoader />
    </div>
  );
}
