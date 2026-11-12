export const ROOM = 6;
export const GAP = 3;
export const DESK_Z = -1.9;
const SEAT_Z = -0.9;
const GUEST_X = 1.5;
const GUEST_Z = 0.6;
const PITCH = ROOM + GAP;

export interface Point {
  x: number;
  z: number;
}

export interface RoomSlot {
  index: number;
  center: Point;
  door: Point;
  corridor: Point;
  lane: number;
  deskSeat: Point;
  guestSpot: Point;
}

export function orderEmployees(
  employees: { agentKey: string; joinedAt?: string }[],
  leadKey: string | null,
): string[] {
  return [...employees]
    .sort((a, b) => {
      if (a.agentKey === leadKey) return -1;
      if (b.agentKey === leadKey) return 1;
      const joined = (a.joinedAt ?? "").localeCompare(b.joinedAt ?? "");
      return joined !== 0 ? joined : a.agentKey.localeCompare(b.agentKey);
    })
    .map((e) => e.agentKey);
}

export function layoutOffice(keys: string[]): Map<string, RoomSlot> {
  const cols = Math.max(1, Math.ceil(Math.sqrt(keys.length)));
  const rows = Math.ceil(keys.length / cols);
  const x0 = (-(cols - 1) * PITCH) / 2;
  const z0 = (-(rows - 1) * PITCH) / 2;
  const slots = new Map<string, RoomSlot>();
  keys.forEach((key, index) => {
    const x = x0 + (index % cols) * PITCH;
    const z = z0 + Math.floor(index / cols) * PITCH;
    slots.set(key, {
      index,
      center: { x, z },
      door: { x, z: z + ROOM / 2 },
      corridor: { x, z: z + PITCH / 2 },
      lane: x + PITCH / 2,
      deskSeat: { x, z: z + SEAT_Z },
      guestSpot: { x: x + GUEST_X, z: z + GUEST_Z },
    });
  });
  return slots;
}

export function tripPath(from: RoomSlot, to: RoomSlot): Point[] {
  const path = [from.door, from.corridor];
  if (from.corridor.z !== to.corridor.z) {
    path.push({ x: from.lane, z: from.corridor.z }, { x: from.lane, z: to.corridor.z });
  }
  path.push(to.corridor, to.door, to.guestSpot);
  return path;
}
