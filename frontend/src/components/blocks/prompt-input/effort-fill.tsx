/*
 * Adapted from the HextaUI Prompt Input block (effort-fill.tsx), https://hextaui.com/blocks/prompt-input.
 * Used under the HextaUI Pro License. See docs/research/upstream-attribution.md.
 */
import { useEffect, useRef, useState, type RefObject } from "react";
import { cn } from "@/lib/utils";
import { prefersReducedMotion } from "./context";

type Palette = [string, string, string];

// Shader colours (start, end, glow) per effort tier; WebGL needs literal RGB, so these sit outside the token layer.
const palettes: Palette[] = [
  ["#d4d4d8", "#a1a1aa", "#f4f4f5"],
  ["#93c5fd", "#3b82f6", "#dbeafe"],
  ["#a5b4fc", "#6366f1", "#e0e7ff"],
  ["#a78bfa", "#7c3aed", "#ede9fe"],
  ["#60a5fa", "#a855f7", "#f5d0fe"],
];

// Without WebGL (or with it refused) the tiers fall back to token gradients.
const fallbacks = [
  "bg-linear-to-r from-foreground/5 to-foreground/20",
  "bg-linear-to-r from-chart-1/30 to-chart-1",
  "bg-linear-to-r from-chart-4/30 to-chart-4/80",
  "bg-linear-to-r from-chart-4/40 to-chart-4",
  "bg-linear-to-r from-chart-1 via-chart-4 to-chart-5",
];

const vertexShader = `
attribute vec2 position;
void main() {
  gl_Position = vec4(position, 0.0, 1.0);
}
`;

const fragmentShader = `
precision mediump float;
uniform vec2 resolution;
uniform float time;
uniform float level;
uniform vec3 start;
uniform vec3 end;
uniform vec3 glow;

float hash(vec2 p) {
  return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453);
}

float noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(hash(i), hash(i + vec2(1.0, 0.0)), u.x),
    mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), u.x),
    u.y
  );
}

float fbm(vec2 p) {
  float value = 0.0;
  float amplitude = 0.5;
  for (int i = 0; i < 4; i++) {
    value += amplitude * noise(p);
    p = p * 2.02 + vec2(3.1, 1.7);
    amplitude *= 0.5;
  }
  return value;
}

void main() {
  vec2 uv = gl_FragCoord.xy / resolution;
  float aspect = resolution.x / resolution.y;
  float speed = 0.18 + 1.4 * level;
  vec2 p = vec2(uv.x * aspect * 0.55, uv.y * 0.9);
  vec2 warp = vec2(
    fbm(p + vec2(-time * speed, time * 0.15)),
    fbm(p + vec2(4.3, 2.1) - vec2(time * speed * 0.8, 0.0))
  );
  float field = fbm(p * 1.4 + warp * 1.6 - vec2(time * speed * 1.2, 0.0));

  vec3 color = mix(start, end, smoothstep(0.0, 1.0, uv.x));
  float sheen = smoothstep(0.35, 0.95, field) * (0.35 + 0.65 * uv.x);
  color = mix(color, glow, sheen * (0.35 + 0.45 * level));

  float core = exp(-pow((1.0 - uv.x) * aspect * 1.6, 2.0)) * level;
  color = mix(color, glow, core * 0.55);

  float sparkle = 0.0;
  if (level > 0.6) {
    vec2 cell = floor(vec2(uv.x * aspect * 7.0 - time * 0.6, uv.y * 7.0));
    float seed = hash(cell);
    vec2 local = fract(vec2(uv.x * aspect * 7.0 - time * 0.6, uv.y * 7.0)) - 0.5;
    float twinkle = pow(0.5 + 0.5 * sin(time * (2.5 + seed * 5.0) + seed * 40.0), 4.0);
    float dot = smoothstep(0.2, 0.0, length(local + (vec2(hash(cell + 3.1), hash(cell + 7.7)) - 0.5) * 0.5));
    sparkle = step(0.55, seed) * dot * twinkle * smoothstep(0.6, 1.0, level) * smoothstep(0.15, 0.6, uv.x);
  }
  color = mix(color, vec3(1.0), sparkle);

  float alpha = smoothstep(0.0, 0.45, uv.x) * (0.55 + 0.45 * level);
  alpha = max(alpha, sparkle);
  gl_FragColor = vec4(color * alpha, alpha);
}
`;

function toRgb(hex: string): [number, number, number] {
  const value = Number.parseInt(hex.replace("#", ""), 16);
  return [((value >> 16) & 255) / 255, ((value >> 8) & 255) / 255, (value & 255) / 255];
}

function compile(gl: WebGLRenderingContext, type: number, source: string) {
  const shader = gl.createShader(type);
  if (!shader) return null;
  gl.shaderSource(shader, source);
  gl.compileShader(shader);
  return gl.getShaderParameter(shader, gl.COMPILE_STATUS) ? shader : null;
}

interface Target { level: number; palette: Palette }

function startFill(canvas: HTMLCanvasElement, target: RefObject<Target>) {
  if (typeof WebGLRenderingContext === "undefined") return null;
  const gl = canvas.getContext("webgl", { alpha: true, premultipliedAlpha: true, antialias: false, powerPreference: "low-power" });
  if (!gl) return null;
  const vertex = compile(gl, gl.VERTEX_SHADER, vertexShader);
  const fragment = compile(gl, gl.FRAGMENT_SHADER, fragmentShader);
  const program = gl.createProgram();
  if (!vertex || !fragment || !program) return null;
  gl.attachShader(program, vertex);
  gl.attachShader(program, fragment);
  gl.linkProgram(program);
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) return null;
  gl.useProgram(program);

  const buffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
  const position = gl.getAttribLocation(program, "position");
  gl.enableVertexAttribArray(position);
  gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);

  const uniforms = {
    resolution: gl.getUniformLocation(program, "resolution"),
    time: gl.getUniformLocation(program, "time"),
    level: gl.getUniformLocation(program, "level"),
    start: gl.getUniformLocation(program, "start"),
    end: gl.getUniformLocation(program, "end"),
    glow: gl.getUniformLocation(program, "glow"),
  };

  const ratio = Math.min(2, window.devicePixelRatio || 1);
  const resize = () => {
    const width = Math.max(1, Math.round(canvas.clientWidth * ratio));
    const height = Math.max(1, Math.round(canvas.clientHeight * ratio));
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
      gl.viewport(0, 0, width, height);
    }
    gl.uniform2f(uniforms.resolution, width, height);
  };

  const still = prefersReducedMotion();
  const current = { level: target.current.level, colors: target.current.palette.map(toRgb) };
  const offset = Math.random() * 50;
  let previous = performance.now();
  let frame = 0;

  const draw = (now: number) => {
    const blend = still ? 1 : 1 - Math.exp(-((now - previous) / 1000) * 9);
    previous = now;
    const goal = target.current;
    current.level += (goal.level - current.level) * blend;
    current.colors = current.colors.map((rgb, index) => {
      const next = toRgb(goal.palette[index]!);
      return rgb.map((channel, part) => channel + (next[part]! - channel) * blend) as [number, number, number];
    });
    resize();
    gl.uniform1f(uniforms.time, still ? offset : offset + now / 1000);
    gl.uniform1f(uniforms.level, current.level);
    gl.uniform3fv(uniforms.start, current.colors[0]!);
    gl.uniform3fv(uniforms.end, current.colors[1]!);
    gl.uniform3fv(uniforms.glow, current.colors[2]!);
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    frame = requestAnimationFrame(draw);
  };
  frame = requestAnimationFrame(draw);

  return () => {
    cancelAnimationFrame(frame);
    gl.getExtension("WEBGL_lose_context")?.loseContext();
  };
}

/** The animated fill behind the effort slider: calm at the low end, alive at the top. */
export function EffortFill({ level, className }: { level: number; className?: string }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const tier = Math.round(level * (palettes.length - 1));
  const target = useRef<Target>({ level, palette: palettes[tier]! });
  const [failed, setFailed] = useState(false);
  useEffect(() => { target.current = { level, palette: palettes[tier]! }; }, [level, tier]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const canvas = document.createElement("canvas");
    canvas.className = "absolute inset-0 size-full rtl:-scale-x-100";
    host.append(canvas);
    const stop = startFill(canvas, target);
    if (!stop) { canvas.remove(); setFailed(true); return; }
    return () => { stop(); canvas.remove(); };
  }, []);

  return <div ref={hostRef} aria-hidden className={cn("absolute inset-0", failed && fallbacks[tier], className)} />;
}
