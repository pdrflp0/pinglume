import { useEffect, useRef, useState } from "react";

const DURATION_MS = 900;

function prefersReducedMotion(): boolean {
  return typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function useCountUp(target: number | null): number | null {
  const [shown, setShown] = useState<number | null>(null);
  const current = useRef(0);

  useEffect(() => {
    if (target === null) {
      setShown(null);
      return;
    }
    if (typeof requestAnimationFrame !== "function" || prefersReducedMotion()) {
      current.current = target;
      setShown(target);
      return;
    }
    const origin = current.current;
    const start = performance.now();
    let id = 0;
    const step = (now: number) => {
      const k = Math.min(1, (now - start) / DURATION_MS);
      const value = origin + (target - origin) * (1 - Math.pow(1 - k, 3));
      current.current = value;
      setShown(value);
      if (k < 1) id = requestAnimationFrame(step);
    };
    id = requestAnimationFrame(step);
    return () => cancelAnimationFrame(id);
  }, [target]);

  return target === null ? null : (shown ?? 0);
}
