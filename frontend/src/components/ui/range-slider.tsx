/*
 * Adapted from beui RangeSlider: https://beui.dev/r/range-slider.json
 * https://beui.dev/components/motion/range-slider
 * Retains its tick layout and spring-driven fill/handle; a native range input
 * supplies pointer, keyboard and assistive-technology behavior. Motion values
 * use percentage positioning so no ResizeObserver or shared gesture kit is needed.
 *
 * MIT License
 * Copyright (c) 2026 Saurabh Chauhan
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
import { motion, useMotionValue, useReducedMotion, useSpring, useTransform } from "motion/react";
import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";

const SPRING_GLIDE = { stiffness: 700, damping: 50, mass: 0.5 };
const SPRING_BOUNCY = { type: "spring", stiffness: 500, damping: 14, mass: 0.7 } as const;

interface RangeSliderProps {
  value: number;
  onValueChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
  showTicks?: boolean;
  className?: string;
  "aria-label": string;
  formatValueText?: (value: number) => string;
}

export function RangeSlider({ value, onValueChange, min = 0, max = 100, step = 1, disabled = false, showTicks = true, className, "aria-label": ariaLabel, formatValueText }: RangeSliderProps) {
  const reduce = useReducedMotion();
  const [dragging, setDragging] = useState(false);
  const upper = Math.max(min, max);
  const stride = step > 0 ? step : 1;
  const current = Math.min(upper, Math.max(min, value));
  const percent = upper > min ? (current - min) / (upper - min) * 100 : 0;
  const target = useMotionValue(percent);
  useEffect(() => { target.set(percent); }, [percent, target]);
  const smooth = useSpring(target, SPRING_GLIDE);
  const position = reduce ? target : smooth;
  const thumbX = useTransform(position, (p) => `calc(${p}% - ${p * 0.24}px)`);
  const fillX = useTransform(position, (p) => `calc(${p - 100}% + ${24 - p * 0.24}px)`);
  const steps = Math.floor(Number(((upper - min) / stride).toFixed(6)));
  const ticks = showTicks && steps > 0 && steps <= 50
    ? Array.from({ length: steps + 1 }, (_, i) => Number((min + i * stride).toFixed(6))) : [];

  return (
    <div className={cn("relative flex h-10 w-full touch-none select-none items-center rounded-full bg-muted ring-1 ring-inset ring-border has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-ring", disabled && "opacity-50", className)}>
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 overflow-hidden rounded-full">
        <motion.div className="absolute inset-0 rounded-full bg-primary/30" style={{ x: fillX }} />
      </div>
      <div aria-hidden="true" className="pointer-events-none absolute inset-x-3 inset-y-0">
        {ticks.map((tick) => <span key={tick} className="absolute top-1/2 size-1 -translate-x-1/2 -translate-y-1/2 rounded-full bg-foreground/40" style={{ left: `${(tick - min) / (upper - min) * 100}%` }} />)}
      </div>
      <motion.div aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-1 h-8" style={{ x: thumbX }}>
        <motion.div className="h-8 w-6 rounded-full bg-foreground" animate={reduce ? undefined : { scaleY: dragging ? 1.12 : 1 }} transition={SPRING_BOUNCY} />
      </motion.div>
      <input
        type="range"
        aria-label={ariaLabel}
        aria-valuetext={formatValueText?.(current)}
        min={min}
        max={upper}
        step={stride}
        value={current}
        disabled={disabled || upper === min}
        onChange={(event) => onValueChange(event.currentTarget.valueAsNumber)}
        onPointerDown={() => setDragging(true)}
        onPointerUp={() => setDragging(false)}
        onPointerCancel={() => setDragging(false)}
        onLostPointerCapture={() => setDragging(false)}
        onBlur={() => setDragging(false)}
        className="absolute inset-0 m-0 h-full w-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
      />
    </div>
  );
}
