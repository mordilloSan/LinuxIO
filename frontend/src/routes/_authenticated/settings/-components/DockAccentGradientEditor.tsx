import {
  useRef,
  useState,
  type ChangeEvent,
  type CSSProperties,
  type KeyboardEvent,
} from "react";

import type { ConfigDockAccentGradient } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import {
  DEFAULT_DOCK_ACCENT_GRADIENT,
  dockAccentGradientCss,
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

type RangeStyle = CSSProperties & {
  "--dock-accent-handle-color": string;
};

type CompleteDockAccentGradient = Required<ConfigDockAccentGradient>;
type RangeDraft = Pick<CompleteDockAccentGradient, "rangeStart" | "rangeEnd">;

const PREVIEW_TILE_COUNT = 9;
const RANGE_STEP = 5;
const RANGE_ADJUSTMENT_KEYS = new Set([
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "ArrowUp",
  "End",
  "Home",
  "PageDown",
  "PageUp",
]);

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
  const committed = completeGradient(value);
  const [rangeDraft, setRangeDraftState] = useState<RangeDraft | null>(null);
  const rangeDraftRef = useRef<RangeDraft | null>(null);
  const stored = rangeDraft ? { ...committed, ...rangeDraft } : committed;
  const resolved = resolveDockAccentGradient(accent, stored);
  const startInputColor =
    toHexColor(resolved.startColor) ?? toHexColor(accent) ?? "#2196f3";
  const endInputColor =
    toHexColor(resolved.endColor) ?? toHexColor(accent) ?? "#2196f3";
  const startHandleColor = sampleDockAccentColor(accent, stored, 0);
  const endHandleColor = sampleDockAccentColor(accent, stored, 1);
  const isDefault =
    !stored.startColor &&
    !stored.endColor &&
    stored.rangeStart === DEFAULT_DOCK_ACCENT_GRADIENT.rangeStart &&
    stored.rangeEnd === DEFAULT_DOCK_ACCENT_GRADIENT.rangeEnd;

  const setRangeDraft = (next: RangeDraft | null) => {
    rangeDraftRef.current = next;
    setRangeDraftState(next);
  };

  const updateColor = (patch: Partial<CompleteDockAccentGradient>) => {
    setRangeDraft(null);
    onChange({ ...stored, ...patch });
  };

  const updateStartRange = (event: ChangeEvent<HTMLInputElement>) => {
    setRangeDraft({
      rangeStart: Math.min(
        Number.parseInt(event.currentTarget.value, 10),
        stored.rangeEnd,
      ),
      rangeEnd: stored.rangeEnd,
    });
  };

  const updateEndRange = (event: ChangeEvent<HTMLInputElement>) => {
    setRangeDraft({
      rangeStart: stored.rangeStart,
      rangeEnd: Math.max(
        Number.parseInt(event.currentTarget.value, 10),
        stored.rangeStart,
      ),
    });
  };

  const commitRange = () => {
    const draft = rangeDraftRef.current;
    if (!draft) return;
    setRangeDraft(null);
    onChange({ ...committed, ...draft });
  };

  const commitRangeOnKeyUp = (event: KeyboardEvent<HTMLInputElement>) => {
    if (RANGE_ADJUSTMENT_KEYS.has(event.key)) commitRange();
  };

  const previewColors = Array.from({ length: PREVIEW_TILE_COUNT }, (_, index) =>
    sampleDockAccentColor(accent, stored, index / (PREVIEW_TILE_COUNT - 1)),
  );

  return (
    <div className="dock-accent-editor">
      <div className="dock-accent-editor__intro">
        <div>
          <AppTypography fontWeight={600} variant="body2">
            Accent palette
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Pick the two edge colors, then keep exactly the part of the blend
            you like.
          </AppTypography>
        </div>
        <AppButton
          color="inherit"
          disabled={isDefault}
          onClick={() => {
            setRangeDraft(null);
            onChange({ ...DEFAULT_DOCK_ACCENT_GRADIENT });
          }}
          size="small"
          variant="text"
        >
          Reset
        </AppButton>
      </div>

      <div aria-hidden="true" className="dock-accent-editor__preview">
        {previewColors.map((color, index) => (
          <span
            className="dock-accent-editor__preview-tile"
            key={index}
            style={{
              background: `linear-gradient(180deg, ${lighten(color, 0.26)}, ${color})`,
            }}
          />
        ))}
      </div>

      <div
        aria-label="Gradient edge colors"
        className="dock-accent-editor__endpoints"
        role="group"
      >
        <label className="dock-accent-editor__endpoint">
          <input
            aria-label="Start color for the full dock gradient"
            className="dock-accent-editor__color-input"
            onChange={(event) =>
              updateColor({ startColor: event.target.value })
            }
            type="color"
            value={startInputColor}
          />
          <span>
            <span className="dock-accent-editor__endpoint-label">
              Gradient start
            </span>
            <span className="dock-accent-editor__endpoint-value">
              {startInputColor}
            </span>
          </span>
        </label>

        <span aria-hidden="true" className="dock-accent-editor__blend-line" />

        <label className="dock-accent-editor__endpoint dock-accent-editor__endpoint--end">
          <span>
            <span className="dock-accent-editor__endpoint-label">
              Gradient end
            </span>
            <span className="dock-accent-editor__endpoint-value">
              {endInputColor}
            </span>
          </span>
          <input
            aria-label="End color for the full dock gradient"
            className="dock-accent-editor__color-input"
            onChange={(event) => updateColor({ endColor: event.target.value })}
            type="color"
            value={endInputColor}
          />
        </label>
      </div>

      <fieldset className="dock-accent-editor__range-fieldset">
        <legend className="dock-accent-editor__range-heading">
          <span>Palette range</span>
          <output>
            {stored.rangeStart}% – {stored.rangeEnd}%
          </output>
        </legend>
        <AppTypography
          className="dock-accent-editor__range-help"
          color="text.secondary"
          variant="caption"
        >
          Drag either end to focus the blend. The preview moves live, and the
          dock updates when you release.
        </AppTypography>
        <div className="dock-accent-editor__range">
          <div
            aria-hidden="true"
            className="dock-accent-editor__range-rail"
            style={{ background: dockAccentGradientCss(accent, stored) }}
          >
            <span
              className="dock-accent-editor__range-muted dock-accent-editor__range-muted--start"
              style={{ width: `${stored.rangeStart}%` }}
            />
            <span
              className="dock-accent-editor__range-selection"
              style={{
                left: `${stored.rangeStart}%`,
                right: `${100 - stored.rangeEnd}%`,
              }}
            />
            <span
              className="dock-accent-editor__range-muted dock-accent-editor__range-muted--end"
              style={{ width: `${100 - stored.rangeEnd}%` }}
            />
          </div>
          <input
            aria-label="Start of dock palette range"
            aria-valuetext={`Palette starts at ${stored.rangeStart}% of the full gradient`}
            className="dock-accent-editor__range-input"
            max={100}
            min={0}
            onBlur={commitRange}
            onChange={updateStartRange}
            onKeyUp={commitRangeOnKeyUp}
            onPointerCancel={commitRange}
            onPointerUp={commitRange}
            step={RANGE_STEP}
            style={
              {
                "--dock-accent-handle-color": startHandleColor,
              } as RangeStyle
            }
            type="range"
            value={stored.rangeStart}
          />
          <input
            aria-label="End of dock palette range"
            aria-valuetext={`Palette ends at ${stored.rangeEnd}% of the full gradient`}
            className="dock-accent-editor__range-input"
            max={100}
            min={0}
            onBlur={commitRange}
            onChange={updateEndRange}
            onKeyUp={commitRangeOnKeyUp}
            onPointerCancel={commitRange}
            onPointerUp={commitRange}
            step={RANGE_STEP}
            style={
              {
                "--dock-accent-handle-color": endHandleColor,
              } as RangeStyle
            }
            type="range"
            value={stored.rangeEnd}
          />
        </div>
      </fieldset>
    </div>
  );
}

export default DockAccentGradientEditor;
