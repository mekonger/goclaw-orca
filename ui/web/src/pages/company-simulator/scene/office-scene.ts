import {
  DirectionalLight,
  HemisphereLight,
  type Object3D,
  PerspectiveCamera,
  Raycaster,
  Scene,
  Vector2,
  WebGLRenderer,
} from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";
import { CSS2DRenderer } from "three/addons/renderers/CSS2DRenderer.js";
import { GAP, ROOM, type RoomSlot, layoutOffice, orderEmployees, tripPath } from "../lib/office-layout";
import type { HandoffTrip, SimulationState } from "../lib/simulation-reducer";
import { DelegationLinks } from "./delegation-links";
import { tickEmployee } from "./employee-animations";
import { EmployeeMesh, type Step } from "./employee-mesh";
import { RoomMesh } from "./room-mesh";
import { disposeObject } from "./scene-utils";

const HANDOFF_SECONDS = 3;
const CLICK_SLOP = 5;

export class OfficeScene {
  private readonly renderer: WebGLRenderer;
  private readonly labels = new CSS2DRenderer();
  private readonly scene = new Scene();
  private readonly camera = new PerspectiveCamera(40, 1, 0.1, 500);
  private readonly controls: OrbitControls;
  private readonly observer: ResizeObserver;
  private readonly rooms = new Map<string, RoomMesh>();
  private readonly employees = new Map<string, EmployeeMesh>();
  private readonly links = new DelegationLinks(this.scene);
  private readonly tripQueues = new Map<string, HandoffTrip[]>();
  private readonly reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  private slots = new Map<string, RoomSlot>();
  private order = "";
  private lastTripSeq: number | null = null;
  private raf = 0;
  private last = 0;
  private downAt = { x: 0, y: 0 };

  constructor(
    private readonly container: HTMLElement,
    private readonly onSelect: (agentKey: string) => void,
  ) {
    this.renderer = new WebGLRenderer({ antialias: true, alpha: true });
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    const canvas = this.renderer.domElement;
    canvas.style.display = "block";
    const overlay = this.labels.domElement;
    overlay.style.cssText = "position:absolute;inset:0;pointer-events:none;overflow:hidden";
    container.append(canvas, overlay);

    const sun = new DirectionalLight(0xffffff, 1.4);
    sun.position.set(8, 16, 10);
    this.scene.add(new HemisphereLight(0xffffff, 0xb8b0d0, 1.6), sun);

    this.controls = new OrbitControls(this.camera, canvas);
    this.controls.maxPolarAngle = Math.PI / 2.2;
    this.controls.minDistance = 6;
    this.controls.maxDistance = 200;

    canvas.addEventListener("pointerdown", this.onPointerDown);
    canvas.addEventListener("pointerup", this.onPointerUp);
    document.addEventListener("visibilitychange", this.onVisibility);
    this.observer = new ResizeObserver(this.resize);
    this.observer.observe(container);
    this.resize();
    this.start();
  }

  sync(state: SimulationState, selected: string | null): void {
    const keys = orderEmployees(Object.values(state.employees), state.leadKey);
    const order = keys.join("\n");
    if (order !== this.order) {
      this.order = order;
      this.slots = layoutOffice(keys);
      this.prune(this.rooms);
      this.prune(this.employees);
      this.links.clear();
      for (const mesh of this.employees.values()) {
        mesh.steps = [];
        mesh.pauseLeft = 0;
        mesh.handoffTo = null;
        mesh.carrying = false;
      }
      this.fitCamera();
    }

    for (const key of keys) {
      const emp = state.employees[key];
      const slot = this.slots.get(key);
      if (!emp || !slot) continue;
      let room = this.rooms.get(key);
      if (!room) {
        room = new RoomMesh(emp);
        this.rooms.set(key, room);
        this.scene.add(room.group);
      }
      room.group.position.set(slot.center.x, 0, slot.center.z);
      room.setLabel(emp);
      room.setSelected(key === selected);

      let mesh = this.employees.get(key);
      if (!mesh) {
        mesh = new EmployeeMesh(key);
        this.employees.set(key, mesh);
        this.scene.add(mesh.group);
      }
      mesh.home = slot.deskSeat;
      mesh.status = emp.status;
      mesh.setBubble(emp.bubble);
    }

    if (this.lastTripSeq !== null) {
      for (const trip of state.trips) {
        if (trip.seq <= this.lastTripSeq) continue;
        const queue = this.tripQueues.get(trip.from) ?? [];
        queue.push(trip);
        this.tripQueues.set(trip.from, queue);
      }
    }
    this.lastTripSeq = state.nextTripSeq - 1;

    this.links.sync(state.delegations, this.slots);
  }

  dispose(): void {
    cancelAnimationFrame(this.raf);
    this.observer.disconnect();
    document.removeEventListener("visibilitychange", this.onVisibility);
    this.renderer.domElement.removeEventListener("pointerdown", this.onPointerDown);
    this.renderer.domElement.removeEventListener("pointerup", this.onPointerUp);
    this.controls.dispose();
    for (const child of [...this.scene.children]) disposeObject(child);
    this.renderer.forceContextLoss();
    this.renderer.dispose();
    this.renderer.domElement.remove();
    this.labels.domElement.remove();
  }

  private prune(items: Map<string, { group: Object3D }>): void {
    for (const [key, item] of items) {
      if (this.slots.has(key)) continue;
      disposeObject(item.group);
      items.delete(key);
      this.tripQueues.delete(key);
    }
  }

  private startTrip(key: string, mesh: EmployeeMesh): void {
    const queue = this.tripQueues.get(key);
    while (queue?.length) {
      const trip = queue.shift();
      const from = this.slots.get(key);
      const to = trip ? this.slots.get(trip.to) : undefined;
      if (!trip || !from || !to) continue;
      const out: Step[] = tripPath(from, to);
      const way = out.slice(0, -1);
      const arrive = { ...out[out.length - 1]!, pause: HANDOFF_SECONDS, handoffTo: trip.to };
      mesh.steps = [...way, arrive, ...way.reverse()];
      mesh.carrying = true;
      return;
    }
  }

  private tick(dt: number, t: number): void {
    for (const mesh of this.employees.values()) mesh.waving = false;
    for (const [key, mesh] of this.employees) {
      if (mesh.steps.length === 0 && mesh.pauseLeft <= 0) this.startTrip(key, mesh);
      const target = mesh.pauseLeft > 0 && mesh.handoffTo ? this.employees.get(mesh.handoffTo) : undefined;
      if (target) target.waving = true;
    }
    for (const mesh of this.employees.values()) tickEmployee(mesh, dt, t, this.reducedMotion);
    this.links.setOpacity(this.reducedMotion ? 1 : 0.65 + Math.sin(t * 4) * 0.35);
  }

  private fitCamera(): void {
    const cols = Math.max(1, Math.ceil(Math.sqrt(this.slots.size)));
    const span = cols * (ROOM + GAP);
    this.controls.target.set(0, 0, 0);
    this.camera.position.set(span * 0.5, span * 1.1 + 6, span * 1.1 + 8);
    this.controls.update();
  }

  private start(): void {
    cancelAnimationFrame(this.raf);
    this.last = performance.now();
    const loop = (now: number) => {
      this.raf = requestAnimationFrame(loop);
      const dt = Math.min((now - this.last) / 1000, 0.1);
      this.last = now;
      this.tick(dt, now / 1000);
      this.controls.update();
      this.renderer.render(this.scene, this.camera);
      this.labels.render(this.scene, this.camera);
    };
    this.raf = requestAnimationFrame(loop);
  }

  private readonly onVisibility = (): void => {
    if (document.hidden) cancelAnimationFrame(this.raf);
    else this.start();
  };

  private readonly resize = (): void => {
    const width = this.container.clientWidth || 1;
    const height = this.container.clientHeight || 1;
    this.renderer.setSize(width, height);
    this.labels.setSize(width, height);
    this.camera.aspect = width / height;
    this.camera.updateProjectionMatrix();
  };

  private readonly onPointerDown = (e: PointerEvent): void => {
    this.downAt = { x: e.clientX, y: e.clientY };
  };

  private readonly onPointerUp = (e: PointerEvent): void => {
    if (e.button !== 0) return;
    if (Math.hypot(e.clientX - this.downAt.x, e.clientY - this.downAt.y) > CLICK_SLOP) return;
    const rect = this.renderer.domElement.getBoundingClientRect();
    const point = new Vector2(
      ((e.clientX - rect.left) / rect.width) * 2 - 1,
      -((e.clientY - rect.top) / rect.height) * 2 + 1,
    );
    const ray = new Raycaster();
    ray.setFromCamera(point, this.camera);
    const targets = [...this.rooms.values(), ...this.employees.values()].map((item) => item.group);
    for (let obj: Object3D | null = ray.intersectObjects(targets, true)[0]?.object ?? null; obj; obj = obj.parent) {
      const key = obj.userData.agentKey as string | undefined;
      if (key) {
        this.onSelect(key);
        return;
      }
    }
  };
}
