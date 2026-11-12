import {
  BoxGeometry,
  CapsuleGeometry,
  CircleGeometry,
  Color,
  Group,
  Mesh,
  MeshBasicMaterial,
  MeshToonMaterial,
  SphereGeometry,
  type BufferGeometry,
  type Material,
} from "three";
import type { CSS2DObject } from "three/addons/renderers/CSS2DRenderer.js";
import type { Point } from "../lib/office-layout";
import type { Bubble, EmployeeStatus } from "../lib/simulation-reducer";
import { agentHue, labelObject } from "./scene-utils";

export type Pose = "work" | "sleep" | "walk" | "handoff";

export interface Step extends Point {
  pause?: number;
  handoffTo?: string;
}

const BUBBLE_CLASS =
  "pointer-events-none w-max max-w-[200px] whitespace-pre-wrap break-words rounded-xl border bg-popover px-2 py-1 text-xs leading-snug text-popover-foreground shadow-md";
const ZZZ_CLASS = "pointer-events-none text-sm font-bold text-sky-400";

export class EmployeeMesh {
  readonly group = new Group();
  readonly rig = new Group();
  readonly eyes: Mesh[];
  readonly arms: Mesh[];
  readonly parcel: Mesh;
  readonly zzz: CSS2DObject;
  readonly phase: number;
  private readonly bubble: CSS2DObject;

  home: Point = { x: 0, z: 0 };
  steps: Step[] = [];
  pauseLeft = 0;
  handoffTo: string | null = null;
  carrying = false;
  waving = false;
  placed = false;
  status: EmployeeStatus = "sleeping";
  pose: Pose = "sleep";

  constructor(agentKey: string) {
    const hue = agentHue(agentKey);
    this.phase = hue * Math.PI * 2;
    this.group.userData.agentKey = agentKey;
    this.group.add(this.rig);

    const skin = new MeshToonMaterial({ color: new Color().setHSL(hue, 0.65, 0.74) });
    const dark = new MeshBasicMaterial({ color: 0x1f2937 });
    const blush = new MeshBasicMaterial({ color: 0xff9eb5 });

    this.part(new CapsuleGeometry(0.38, 0.45, 6, 14), skin, 0, 0.6, 0);
    this.part(new SphereGeometry(0.46, 20, 16), skin.clone(), 0, 1.5, 0);
    this.eyes = [-0.17, 0.17].map((x) => this.part(new SphereGeometry(0.06, 10, 8), dark.clone(), x, 1.53, 0.41));
    for (const x of [-0.28, 0.28]) {
      this.part(new CircleGeometry(0.075, 12), blush.clone(), x, 1.4, 0.38).rotation.y = x * 2.2;
    }
    this.arms = [-0.47, 0.47].map((x) => this.part(new CapsuleGeometry(0.09, 0.26, 4, 8), skin.clone(), x, 0.78, 0.05));
    this.parcel = this.part(new BoxGeometry(0.32, 0.24, 0.24), new MeshToonMaterial({ color: 0xfbbf24 }), 0, 0.82, 0.5);
    this.parcel.visible = false;

    this.bubble = labelObject(BUBBLE_CLASS);
    this.bubble.center.set(0.5, 1);
    this.bubble.position.set(0, 2.25, 0);
    this.bubble.visible = false;
    this.zzz = labelObject(ZZZ_CLASS, "Zzz");
    this.zzz.position.set(0.5, 2.1, 0);
    this.group.add(this.bubble, this.zzz);
  }

  setBubble(bubble: Bubble | null): void {
    this.bubble.visible = bubble !== null;
    if (!bubble) return;
    const text = `${bubble.kind === "tool" ? "🔧" : "💭"} ${bubble.text}`;
    if (this.bubble.element.textContent !== text) this.bubble.element.textContent = text;
  }

  private part(geometry: BufferGeometry, material: Material, x: number, y: number, z: number): Mesh {
    const mesh = new Mesh(geometry, material);
    mesh.position.set(x, y, z);
    this.rig.add(mesh);
    return mesh;
  }
}
