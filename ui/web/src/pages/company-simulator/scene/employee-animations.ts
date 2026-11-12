import type { EmployeeMesh, Pose } from "./employee-mesh";

const SPEED = 3.2;
const SLEEP_TILT = 0.9;

function advance(m: EmployeeMesh, dt: number, reducedMotion: boolean): boolean {
  if (m.pauseLeft > 0) {
    m.pauseLeft -= dt;
    if (m.pauseLeft <= 0) {
      m.handoffTo = null;
      m.carrying = false;
    }
    return false;
  }
  const target = m.steps[0] ?? m.home;
  const pos = m.group.position;
  const dx = target.x - pos.x;
  const dz = target.z - pos.z;
  const dist = Math.hypot(dx, dz);
  const reach = reducedMotion || !m.placed ? Infinity : SPEED * dt;
  m.placed = true;
  if (dist > reach) {
    pos.x += (dx / dist) * reach;
    pos.z += (dz / dist) * reach;
    m.group.rotation.y = Math.atan2(dx, dz);
    return true;
  }
  pos.x = target.x;
  pos.z = target.z;
  const done = m.steps.shift();
  if (done?.pause) {
    m.pauseLeft = done.pause;
    m.handoffTo = done.handoffTo ?? null;
  }
  return m.steps.length > 0;
}

function poseOf(m: EmployeeMesh, moving: boolean): Pose {
  if (m.pauseLeft > 0) return "handoff";
  if (moving) return "walk";
  return m.status === "working" ? "work" : "sleep";
}

export function tickEmployee(m: EmployeeMesh, dt: number, t: number, reducedMotion: boolean): void {
  const pose = poseOf(m, advance(m, dt, reducedMotion));
  m.pose = pose;
  const amp = reducedMotion ? 0 : 1;
  const seated = pose === "work" || pose === "sleep";
  if (seated) m.group.rotation.y = Math.PI;

  const bob =
    pose === "walk" ? Math.abs(Math.sin(t * 10)) * 0.08 : pose === "work" ? Math.sin(t * 8 + m.phase) * 0.03 : 0;
  m.rig.position.y = bob * amp;

  const tilt = pose === "sleep" ? SLEEP_TILT : 0;
  m.rig.rotation.x += (tilt - m.rig.rotation.x) * (reducedMotion ? 1 : Math.min(1, dt * 6));

  for (const eye of m.eyes) eye.scale.y = pose === "sleep" ? 0.1 : 1;

  m.arms.forEach((arm, i) => {
    const sign = i === 0 ? 1 : -1;
    arm.rotation.z = 0;
    switch (pose) {
      case "work":
        arm.rotation.x = -1 + Math.sin(t * 16 + m.phase + i) * 0.3 * amp;
        break;
      case "walk":
        arm.rotation.x = m.carrying ? -1.2 : Math.sin(t * 10) * 0.6 * sign * amp;
        break;
      case "handoff":
        arm.rotation.x = -1.2;
        break;
      default:
        arm.rotation.x = -0.6;
    }
  });
  const waver = m.arms[0];
  if (m.waving && waver) {
    waver.rotation.x = 0;
    waver.rotation.z = -2.4 + Math.sin(t * 12) * 0.4 * amp;
  }

  m.parcel.visible = m.carrying;
  m.zzz.visible = pose === "sleep";
  m.zzz.position.y = 2.1 + Math.sin(t * 2 + m.phase) * 0.12 * amp;
}
