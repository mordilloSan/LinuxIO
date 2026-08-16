import {
  useMotionValue,
  useSpring,
  useTransform,
  type MotionValue,
} from "motion/react";
import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  type ReactNode,
} from "react";

/* Resting tile size, peak size under the cursor, and how far (px) the
   magnification bulge reaches to either side of the cursor. */
export const DOCK_TILE_SIZE = 40;
export const DOCK_TILE_SIZE_MAX = 64;
const MAGNIFY_RANGE = 140;
/* Positive = downward: tiles hang from the top-mounted bar and dip toward
   the cursor. */
const LIFT_MAX = 8;
const SPRING = { damping: 14, mass: 0.1, stiffness: 170 };

export interface DockTarget {
  scale: number;
  x: number;
}

/* The tile layer is rasterized at its 64px peak size and only scaled down.
   Keeping this conversion separate from the visual 1..1.6 scale lets the
   displacement and lift calculations retain their existing geometry. */
export function calculateDockRenderScale(visualScale: number): number {
  return (visualScale * DOCK_TILE_SIZE) / DOCK_TILE_SIZE_MAX;
}

export function calculateDockTargets(
  centers: readonly number[],
  pointer: number,
): DockTarget[] {
  if (!Number.isFinite(pointer)) {
    return centers.map(() => ({ scale: 1, x: 0 }));
  }

  const deltas = centers.map(
    (center) =>
      (DOCK_TILE_SIZE_MAX - DOCK_TILE_SIZE) *
      Math.max(0, 1 - Math.abs(pointer - center) / MAGNIFY_RANGE),
  );
  const total = deltas.reduce((sum, delta) => sum + delta, 0);
  let preceding = 0;

  return deltas.map((delta) => {
    const result = {
      scale: (DOCK_TILE_SIZE + delta) / DOCK_TILE_SIZE,
      x: preceding + delta / 2 - total / 2,
    };
    preceding += delta;
    return result;
  });
}

interface DockRegistration {
  element: HTMLElement | null;
  scale: MotionValue<number>;
  x: MotionValue<number>;
}

interface DockLayoutEntry {
  center: number;
  registration: DockRegistration;
}

interface DockContextValue {
  registerTile: (
    registration: DockRegistration,
    element: HTMLElement | null,
  ) => void;
  setPointer: (pointer: number) => void;
}

const DockContext = createContext<DockContextValue | null>(null);

// Pointer and resize events drive this mutable MotionValue registry directly;
// React renders neither read nor own its per-frame state.
export const DockMagnificationProvider = ({
  children,
}: {
  children: ReactNode;
}) => {
  "use no memo";

  const registrations = useRef(new Set<DockRegistration>());
  const layout = useRef<DockLayoutEntry[]>([]);
  const layoutDirty = useRef(true);
  const pendingPointer = useRef(Infinity);
  const raf = useRef<number | undefined>(undefined);

  const measure = useCallback(() => {
    layout.current = Array.from(registrations.current)
      .flatMap((registration) => {
        if (!registration.element) return [];
        const bounds = registration.element.getBoundingClientRect();
        return [{ center: bounds.left + bounds.width / 2, registration }];
      })
      .sort((a, b) => a.center - b.center);
    layoutDirty.current = false;
  }, []);

  const update = useCallback(() => {
    raf.current = undefined;
    const pointer = pendingPointer.current;

    if (!Number.isFinite(pointer)) {
      for (const registration of registrations.current) {
        registration.scale.set(1);
        registration.x.set(0);
      }
      layoutDirty.current = true;
      return;
    }

    if (layoutDirty.current) measure();
    const targets = calculateDockTargets(
      layout.current.map(({ center }) => center),
      pointer,
    );
    layout.current.forEach(({ registration }, index) => {
      const target = targets[index];
      if (!target) return;
      registration.scale.set(target.scale);
      registration.x.set(target.x);
    });
  }, [measure]);

  const setPointer = useCallback(
    (pointer: number) => {
      pendingPointer.current = pointer;
      if (raf.current === undefined) {
        raf.current = window.requestAnimationFrame(update);
      }
    },
    [update],
  );

  const registerTile = useCallback(
    (registration: DockRegistration, element: HTMLElement | null) => {
      registration.element = element;
      if (element) registrations.current.add(registration);
      else registrations.current.delete(registration);
      layoutDirty.current = true;
    },
    [],
  );

  useEffect(() => {
    const invalidateLayout = () => {
      layoutDirty.current = true;
    };
    window.addEventListener("resize", invalidateLayout);
    return () => {
      window.removeEventListener("resize", invalidateLayout);
      if (raf.current !== undefined) window.cancelAnimationFrame(raf.current);
    };
  }, []);

  const context = useMemo(
    () => ({ registerTile, setPointer }),
    [registerTile, setPointer],
  );
  // The compiler follows callback-captured refs to this provider value even
  // though they are only read by pointer/resize events outside render.
  // oxlint-disable-next-line react/react-compiler
  return createElement(DockContext.Provider, { value: context }, children);
};

const useRequiredDockContext = () => {
  const context = useContext(DockContext);
  if (!context) {
    throw new Error(
      "Dock magnification hooks require DockMagnificationProvider",
    );
  }
  return context;
};

/* Shared magnification physics for anything rendered as a dock tile: the
   tile's size and vertical dip derive from its distance to the cursor. */
export function useDockMagnification() {
  const context = useRequiredDockContext();
  const scaleTarget = useMotionValue(1);
  const xTarget = useMotionValue(0);
  const registration = useMemo<DockRegistration>(
    () => ({ element: null, scale: scaleTarget, x: xTarget }),
    [scaleTarget, xTarget],
  );
  const registerTile = useCallback(
    (element: HTMLElement | null) =>
      context.registerTile(registration, element),
    [context, registration],
  );
  const scale = useSpring(scaleTarget, SPRING);
  const x = useSpring(xTarget, SPRING);
  const renderScale = useTransform(scale, calculateDockRenderScale);
  const lift = useTransform(
    scale,
    [1, DOCK_TILE_SIZE_MAX / DOCK_TILE_SIZE],
    [0, LIFT_MAX],
  );
  const labelY = useTransform(
    scale,
    [1, DOCK_TILE_SIZE_MAX / DOCK_TILE_SIZE],
    [0, DOCK_TILE_SIZE_MAX - DOCK_TILE_SIZE + LIFT_MAX],
  );

  return { labelY, lift, registerTile, renderScale, x };
}

export function useDockPointer() {
  return useRequiredDockContext().setPointer;
}
