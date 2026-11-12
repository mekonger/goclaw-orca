import { BoxGeometry, Color, Group, Mesh, MeshStandardMaterial } from "three";
import type { CSS2DObject } from "three/addons/renderers/CSS2DRenderer.js";
import { DESK_Z, ROOM } from "../lib/office-layout";
import type { EmployeeState } from "../lib/simulation-reducer";
import { agentHue, labelObject } from "./scene-utils";

const WALL_H = 1.2;
const WALL_T = 0.15;
const DOOR_W = 1.8;
const PLATE_CLASS =
  "pointer-events-none whitespace-nowrap rounded-md border bg-background/85 px-2 py-0.5 text-xs font-medium text-foreground shadow-sm";

export class RoomMesh {
  readonly group = new Group();
  private readonly floor: MeshStandardMaterial;
  private readonly base: Color;
  private readonly plate: CSS2DObject;

  constructor(emp: EmployeeState) {
    this.group.userData.agentKey = emp.agentKey;
    this.base = new Color().setHSL(agentHue(emp.agentKey), 0.55, 0.88);
    this.floor = new MeshStandardMaterial({ color: this.base });
    this.box(ROOM, 0.1, ROOM, 0, -0.05, 0, this.floor);

    const wall = new MeshStandardMaterial({ color: 0xece8f4 });
    const half = ROOM / 2;
    const side = (ROOM - DOOR_W) / 2;
    this.box(ROOM, WALL_H, WALL_T, 0, WALL_H / 2, -half, wall);
    this.box(WALL_T, WALL_H, ROOM, -half, WALL_H / 2, 0, wall);
    this.box(WALL_T, WALL_H, ROOM, half, WALL_H / 2, 0, wall.clone());
    this.box(side, WALL_H, WALL_T, -(DOOR_W + side) / 2, WALL_H / 2, half, wall.clone());
    this.box(side, WALL_H, WALL_T, (DOOR_W + side) / 2, WALL_H / 2, half, wall.clone());

    this.box(2, 0.8, 0.9, 0, 0.4, DESK_Z, new MeshStandardMaterial({ color: 0xc9a27a }));
    this.box(0.7, 0.45, 0.06, 0.6, 1.05, DESK_Z - 0.25, new MeshStandardMaterial({ color: 0x334155 }));

    this.plate = labelObject(PLATE_CLASS);
    this.plate.position.set(0, WALL_H + 0.6, -half);
    this.group.add(this.plate);
    this.setLabel(emp);
  }

  setLabel(emp: EmployeeState): void {
    const text = `${emp.emoji ? `${emp.emoji} ` : ""}${emp.displayName}${emp.role === "lead" ? " ★" : ""}`;
    if (this.plate.element.textContent !== text) this.plate.element.textContent = text;
  }

  setSelected(selected: boolean): void {
    if (selected) this.floor.color.setHSL(agentHue(this.group.userData.agentKey as string), 0.8, 0.72);
    else this.floor.color.copy(this.base);
  }

  private box(w: number, h: number, d: number, x: number, y: number, z: number, material: MeshStandardMaterial): void {
    const mesh = new Mesh(new BoxGeometry(w, h, d), material);
    mesh.position.set(x, y, z);
    this.group.add(mesh);
  }
}
