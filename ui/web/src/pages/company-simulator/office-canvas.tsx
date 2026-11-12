import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { SimulationState } from "./lib/simulation-reducer";
import { OfficeScene } from "./scene/office-scene";

interface OfficeCanvasProps {
  state: SimulationState;
  selected: string | null;
  onSelect: (agentKey: string) => void;
}

export function OfficeCanvas({ state, selected, onSelect }: OfficeCanvasProps) {
  const { t } = useTranslation("company-simulator");
  const containerRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<OfficeScene | null>(null);
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;
  const [unsupported, setUnsupported] = useState(false);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let scene: OfficeScene;
    try {
      scene = new OfficeScene(container, (key) => onSelectRef.current(key));
    } catch {
      setUnsupported(true);
      return;
    }
    sceneRef.current = scene;
    return () => {
      scene.dispose();
      sceneRef.current = null;
    };
  }, []);

  useEffect(() => {
    sceneRef.current?.sync(state, selected);
  }, [state, selected]);

  return (
    <div
      ref={containerRef}
      aria-hidden={!unsupported}
      className="relative h-[60dvh] min-h-[320px] w-full touch-none overflow-hidden rounded-lg border bg-gradient-to-b from-sky-100 to-indigo-100 lg:h-full dark:from-slate-800 dark:to-slate-900"
    >
      {unsupported && (
        <p className="absolute inset-0 flex items-center justify-center p-4 text-center text-sm text-muted-foreground">
          {t("webglUnsupported")}
        </p>
      )}
    </div>
  );
}
