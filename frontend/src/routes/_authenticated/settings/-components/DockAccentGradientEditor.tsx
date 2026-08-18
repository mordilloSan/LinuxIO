import type { ConfigDockAccentGradient } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import {
  DEFAULT_DOCK_ACCENT_GRADIENT,
  resolveDockAccentGradient,
  sampleDockAccentColor,
} from "@/routes/_authenticated/-components/dock/dockPalette";
import { lighten, toHexColor } from "@/utils/color";

import "./dock-accent-gradient-editor.css";

interface DockAccentGradientEditorProps {
  accent: string;
  onChange: (value: ConfigDockAccentGradient) => void;
  value?: ConfigDockAccentGradient;
}

type CompleteDockAccentGradient = Required<ConfigDockAccentGradient>;

const PALETTE_TILE_COUNT = 11;

function completeGradient(
  value: ConfigDockAccentGradient | undefined,
): CompleteDockAccentGradient {
  return {
    startColor: value?.startColor ?? DEFAULT_DOCK_ACCENT_GRADIENT.startColor,
    endColor: value?.endColor ?? DEFAULT_DOCK_ACCENT_GRADIENT.endColor,
    rangeStart: value?.rangeStart ?? DEFAULT_DOCK_ACCENT_GRADIENT.rangeStart,
    rangeEnd: value?.rangeEnd ?? DEFAULT_DOCK_ACCENT_GRADIENT.rangeEnd,
  };
}

function DockAccentGradientEditor({
  accent,
  onChange,
  value,
}: DockAccentGradientEditorProps) {
  const stored = completeGradient(value);
  const resolved = resolveDockAccentGradient(accent, stored);
  const startInputColor =
    toHexColor(resolved.startColor) ?? toHexColor(accent) ?? "#2196f3";
  const endInputColor =
    toHexColor(resolved.endColor) ?? toHexColor(accent) ?? "#2196f3";
  const isDefault =
    !stored.startColor &&
    !stored.endColor &&
    stored.rangeStart === DEFAULT_DOCK_ACCENT_GRADIENT.rangeStart &&
    stored.rangeEnd === DEFAULT_DOCK_ACCENT_GRADIENT.rangeEnd;

  const updateColor = (patch: Partial<CompleteDockAccentGradient>) => {
    onChange({ ...stored, ...patch });
  };

  const selectTile = (percent: number) => {
    if (percent < stored.rangeStart) {
      onChange({ ...stored, rangeStart: percent });
    } else if (percent > stored.rangeEnd) {
      onChange({ ...stored, rangeEnd: percent });
    } else if (percent - stored.rangeStart <= stored.rangeEnd - percent) {
      onChange({ ...stored, rangeStart: percent });
    } else {
      onChange({ ...stored, rangeEnd: percent });
    }
  };

  const tiles = Array.from({ length: PALETTE_TILE_COUNT }, (_, index) => {
    const position = index / (PALETTE_TILE_COUNT - 1);
    const percent = Math.round(position * 100);
    return {
      percent,
      included: percent >= stored.rangeStart && percent <= stored.rangeEnd,
      color: sampleDockAccentColor(
        accent,
        { ...stored, rangeStart: 0, rangeEnd: 100 },
        position,
      ),
    };
  });

  return (
    <div className="dock-accent-editor">
      <div className="dock-accent-editor__intro">
        <div>
          <AppTypography fontWeight={600} variant="body2">
            Accent palette
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Pick the two edge colors, then click the tiles where the blend
            should start and end.
          </AppTypography>
        </div>
        <div className="dock-accent-editor__intro-actions">
          <output aria-label="Palette range">
            {stored.rangeStart}% – {stored.rangeEnd}%
          </output>
          <AppButton
            color="inherit"
            disabled={isDefault}
            onClick={() => onChange({ ...DEFAULT_DOCK_ACCENT_GRADIENT })}
            size="small"
            variant="text"
          >
            Reset
          </AppButton>
        </div>
      </div>

      <div className="dock-accent-editor__panel">
        <input
          aria-label="Start color for the full dock gradient"
          className="dock-accent-editor__color-input"
          onChange={(event) => updateColor({ startColor: event.target.value })}
          title={`Gradient start ${startInputColor}`}
          type="color"
          value={startInputColor}
        />

        <div
          aria-label="Palette range"
          className="dock-accent-editor__preview"
          role="group"
        >
          {tiles.map((tile, index) => (
            <button
              aria-label={`Palette stop ${tile.percent}%`}
              aria-pressed={tile.included}
              className={
                tile.included
                  ? "dock-accent-editor__preview-tile"
                  : "dock-accent-editor__preview-tile dock-accent-editor__preview-tile--muted"
              }
              key={index}
              onClick={() => selectTile(tile.percent)}
              style={{
                background: `linear-gradient(180deg, ${lighten(tile.color, 0.26)}, ${tile.color})`,
              }}
              type="button"
            />
          ))}
        </div>

        <input
          aria-label="End color for the full dock gradient"
          className="dock-accent-editor__color-input"
          onChange={(event) => updateColor({ endColor: event.target.value })}
          title={`Gradient end ${endInputColor}`}
          type="color"
          value={endInputColor}
        />
      </div>
    </div>
  );
}

export default DockAccentGradientEditor;
