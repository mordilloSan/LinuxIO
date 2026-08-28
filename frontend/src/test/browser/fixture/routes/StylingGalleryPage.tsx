import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getDialogSurfaceStyles } from "@/theme/surfaces";

// The gallery is photographed, so its icons come from the registered set
// rather than a network fetch; the other fixture pages keep their own setup.
import "@/icons/shell";
import "@fontsource-variable/inter/wght.css";

/*
 * One page that exercises the shared styling surface — the type scale, the
 * palette and spacing tokens, and the components that resolve their colours
 * through --app-* variables — so a screenshot of it in each colour scheme
 * catches a token or component regression that jsdom cannot see. Everything
 * here is static: no data, no time, no hover state.
 */

const TYPE_VARIANTS = [
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "subtitle1",
  "subtitle2",
  "body1",
  "body2",
  "caption",
  "overline",
] as const;
const PALETTE = [
  "primary",
  "secondary",
  "success",
  "warning",
  "error",
  "info",
] as const;
const CHIP_COLORS = ["default", ...PALETTE] as const;
const CHIP_VARIANTS = ["filled", "soft", "outlined"] as const;
const CHIP_SIZES = ["xsmall", "small", "medium"] as const;
const BUTTON_VARIANTS = ["contained", "outlined", "text"] as const;
const BUTTON_COLORS = [
  "primary",
  "secondary",
  "success",
  "warning",
  "error",
] as const;
const SPACE_TOKENS = [2, 4, 6, 8, 12, 16, 20, 24, 32, 48] as const;

const Section = ({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) => (
  <section style={{ display: "grid", gap: "var(--app-space-8)" }}>
    <AppTypography color="text.secondary" variant="overline">
      {title}
    </AppTypography>
    {children}
  </section>
);

const Row = ({ children }: { children: ReactNode }) => (
  <div
    style={{
      display: "flex",
      flexWrap: "wrap",
      alignItems: "center",
      gap: "var(--app-space-8)",
    }}
  >
    {children}
  </div>
);

const StylingGalleryPage = () => (
  <main
    data-testid="styling-gallery"
    style={{
      display: "grid",
      gap: "var(--app-space-24)",
      padding: "var(--app-space-24)",
      background: "var(--app-palette-background-default)",
      color: "var(--app-palette-text-primary)",
      minHeight: "100vh",
    }}
  >
    <Section title="Type scale">
      {TYPE_VARIANTS.map((variant) => (
        <AppTypography key={variant} variant={variant}>
          {variant} — The quick brown fox jumps over the lazy dog
        </AppTypography>
      ))}
    </Section>

    <Section title="Palette">
      <Row>
        {PALETTE.map((name) => (
          <div
            key={name}
            style={{
              width: 96,
              height: 48,
              borderRadius: "var(--app-radius-md)",
              background: `var(--app-palette-${name}-main)`,
              color: `var(--app-palette-${name}-contrast-text)`,
              display: "grid",
              placeItems: "center",
            }}
          >
            <AppTypography component="span" variant="caption">
              {name}
            </AppTypography>
          </div>
        ))}
        {PALETTE.map((name) => (
          <StatusDot color={`var(--app-palette-${name}-main)`} key={name} />
        ))}
      </Row>
    </Section>

    <Section title="Spacing">
      <Row>
        {SPACE_TOKENS.map((token) => (
          <div
            key={token}
            style={{
              width: `var(--app-space-${token})`,
              height: 24,
              background: "var(--app-palette-primary-main)",
              borderRadius: "var(--app-radius-base)",
            }}
            title={`--app-space-${token}`}
          />
        ))}
      </Row>
    </Section>

    <Section title="Buttons">
      {BUTTON_VARIANTS.map((variant) => (
        <Row key={variant}>
          {BUTTON_COLORS.map((color) => (
            <AppButton color={color} key={color} variant={variant}>
              {color}
            </AppButton>
          ))}
          <AppButton size="small" variant={variant}>
            small
          </AppButton>
          <AppButton disabled variant={variant}>
            disabled
          </AppButton>
        </Row>
      ))}
    </Section>

    <Section title="Chips">
      {CHIP_VARIANTS.map((variant) =>
        CHIP_SIZES.map((size) => (
          <Row key={`${variant}-${size}`}>
            {CHIP_COLORS.map((color) => (
              <AppChip
                color={color}
                key={color}
                label={`${color} ${size}`}
                size={size}
                variant={variant}
              />
            ))}
          </Row>
        )),
      )}
    </Section>

    <Section title="Alerts">
      {(["info", "success", "warning", "error"] as const).map((severity) => (
        <AppAlert key={severity} severity={severity}>
          An {severity} alert with a sentence of body text.
        </AppAlert>
      ))}
    </Section>

    <Section title="Surfaces">
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
          gap: "var(--app-space-16)",
        }}
      >
        <FrostedCard style={{ padding: "var(--app-space-12)" }}>
          <AppTypography fontWeight={600} variant="subtitle1">
            Frosted card
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            Resting surface, no accent.
          </AppTypography>
        </FrostedCard>
        <FrostedCard
          accent
          hoverLift
          style={{ padding: "var(--app-space-12)" }}
        >
          <AppTypography fontWeight={600} variant="subtitle1">
            Accent card
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            Accent line, lifts on hover.
          </AppTypography>
        </FrostedCard>
        <div
          style={{
            ...getDialogSurfaceStyles(),
            padding: "var(--app-space-12)",
          }}
        >
          <AppTypography fontWeight={600} variant="subtitle1">
            Dialog surface
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            Paper ringed by the dialog glow.
          </AppTypography>
        </div>
      </div>
    </Section>
  </main>
);

export default StylingGalleryPage;
