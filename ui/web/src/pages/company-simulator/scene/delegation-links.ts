import { BufferGeometry, Line, LineBasicMaterial, QuadraticBezierCurve3, type Scene, Vector3 } from "three";
import type { RoomSlot } from "../lib/office-layout";
import type { Delegation } from "../lib/simulation-reducer";
import { disposeObject } from "./scene-utils";

const LINK_Y = 2;

export class DelegationLinks {
  private readonly lines = new Map<string, Line>();

  constructor(private readonly scene: Scene) {}

  sync(delegations: Delegation[], slots: Map<string, RoomSlot>): void {
    const wanted = new Map(delegations.map((d) => [`${d.from}\n${d.to}`, d]));
    for (const key of [...this.lines.keys()]) if (!wanted.has(key)) this.drop(key);
    for (const [key, d] of wanted) {
      const a = slots.get(d.from)?.center;
      const b = slots.get(d.to)?.center;
      if (this.lines.has(key) || !a || !b) continue;
      const lift = 3 + Math.hypot(a.x - b.x, a.z - b.z) * 0.25;
      const curve = new QuadraticBezierCurve3(
        new Vector3(a.x, LINK_Y, a.z),
        new Vector3((a.x + b.x) / 2, LINK_Y + lift, (a.z + b.z) / 2),
        new Vector3(b.x, LINK_Y, b.z),
      );
      const line = new Line(
        new BufferGeometry().setFromPoints(curve.getPoints(24)),
        new LineBasicMaterial({ color: 0xf59e0b, transparent: true }),
      );
      this.lines.set(key, line);
      this.scene.add(line);
    }
  }

  clear(): void {
    for (const key of [...this.lines.keys()]) this.drop(key);
  }

  setOpacity(opacity: number): void {
    for (const line of this.lines.values()) (line.material as LineBasicMaterial).opacity = opacity;
  }

  private drop(key: string): void {
    const line = this.lines.get(key);
    if (line) disposeObject(line);
    this.lines.delete(key);
  }
}
