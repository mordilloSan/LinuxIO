import AppTypography from "@/components/ui/AppTypography";

interface LogoDisplayProps {
  showText?: boolean;
}

const LogoDisplay = ({ showText = false }: LogoDisplayProps) => {
  return (
    <AppTypography
      component="div"
      fontWeight={400}
      style={{ display: "inline-flex", alignItems: "center", margin: 0 }}
      variant="h3"
    >
      <span
        style={{
          color: "var(--app-palette-text-primary)",
          display: "inline-block",
          whiteSpace: "nowrap",
          opacity: showText ? 1 : 0,
          marginRight: showText ? 8 : -50,
          transition:
            "opacity var(--app-transition-duration-fast) var(--app-easing-standard), margin-right var(--app-transition-duration-fast) var(--app-easing-standard)",
        }}
      >
        Linux
      </span>

      <AppTypography
        color="var(--app-palette-primary-main)"
        component="span"
        fontWeight={900}
        style={{
          width: 36,
          height: 36,
          borderRadius: "50%",
          border: "3px solid var(--app-palette-primary-main)",
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          whiteSpace: "nowrap",
          boxSizing: "border-box",
        }}
        variant="subtitle1"
      >
        i/O
      </AppTypography>
    </AppTypography>
  );
};

export default LogoDisplay;
