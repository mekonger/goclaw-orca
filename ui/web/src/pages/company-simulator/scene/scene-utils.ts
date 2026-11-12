import { type Material, Mesh, Line, type Object3D } from "three";
import { CSS2DObject } from "three/addons/renderers/CSS2DRenderer.js";

export function agentHue(key: string): number {
  let hash = 0;
  for (let i = 0; i < key.length; i++) hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  return (hash % 360) / 360;
}

export function labelObject(className: string, text = ""): CSS2DObject {
  const el = document.createElement("div");
  el.className = className;
  el.textContent = text;
  return new CSS2DObject(el);
}

export function disposeObject(root: Object3D): void {
  root.traverse((obj) => {
    if (obj instanceof CSS2DObject) {
      obj.element.remove();
      return;
    }
    if (obj instanceof Mesh || obj instanceof Line) {
      obj.geometry.dispose();
      const materials: Material[] = Array.isArray(obj.material) ? obj.material : [obj.material];
      for (const m of materials) m.dispose();
    }
  });
  root.removeFromParent();
}
