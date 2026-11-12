import { describe, expect, it } from "vitest";
import { GAP, ROOM, layoutOffice, orderEmployees, tripPath } from "./office-layout";

const keys = (n: number) => Array.from({ length: n }, (_, i) => `agent-${i}`);

describe("office layout", () => {
  it.each([1, 2, 5, 9, 10])("gives %i agents a room each without overlap", (n) => {
    const slots = [...layoutOffice(keys(n)).values()];
    expect(slots).toHaveLength(n);
    for (const a of slots) {
      for (const b of slots) {
        if (a === b) continue;
        const apart = Math.max(Math.abs(a.center.x - b.center.x), Math.abs(a.center.z - b.center.z));
        expect(apart).toBeGreaterThanOrEqual(ROOM + GAP);
      }
    }
  });

  it("puts the lead first, then by join date, then by key", () => {
    const order = orderEmployees(
      [
        { agentKey: "zed", joinedAt: "2026-01-01" },
        { agentKey: "boss", joinedAt: "2026-05-01" },
        { agentKey: "amy", joinedAt: "2026-03-01" },
        { agentKey: "abe", joinedAt: "2026-03-01" },
      ],
      "boss",
    );
    expect(order).toEqual(["boss", "zed", "abe", "amy"]);
  });

  it("keeps existing room indices when a member is added", () => {
    const before = layoutOffice(keys(4));
    const after = layoutOffice(keys(5));
    for (const [key, slot] of before) expect(after.get(key)?.index).toBe(slot.index);
  });

  it("routes a trip through corridors and ends at the guest spot", () => {
    const slots = layoutOffice(keys(4));
    const a = slots.get("agent-0")!;
    const sameRow = tripPath(a, slots.get("agent-1")!);
    const otherRow = tripPath(a, slots.get("agent-3")!);
    expect(sameRow).toHaveLength(5);
    expect(otherRow).toHaveLength(7);
    expect(otherRow[0]).toEqual(a.door);
    expect(otherRow[6]).toEqual(slots.get("agent-3")!.guestSpot);
    for (const p of otherRow.slice(1, 5)) {
      const insideRoom = [...slots.values()].some(
        (s) => Math.abs(p.x - s.center.x) < ROOM / 2 && Math.abs(p.z - s.center.z) < ROOM / 2,
      );
      expect(insideRoom).toBe(false);
    }
  });
});
