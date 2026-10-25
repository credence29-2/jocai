import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { set, useStore, type ContextMenuItem } from "../store";
import type { ID, Status, User } from "../types";
import { Icon } from "./Icon";

const palette = ["#e8743b", "#5865f2", "#3ba55c", "#faa61a", "#ed4245", "#eb459e", "#1abc9c", "#9b59b6"];

export function colorFor(id: string) {
  let h = 0;
  for (const ch of id) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return palette[h % palette.length];
}

export function Avatar({ user, size = 40, status, speaking }: { user?: User | null; size?: number; status?: Status; speaking?: boolean }) {
  const name = user?.display_name || user?.username || "?";
  return (
    <div className={"avatar" + (speaking ? " speaking" : "")} style={{ width: size, height: size }}>
      {user?.avatar ? (
        <img src={user.avatar} alt="" draggable={false} />
      ) : (
        <div className="avatar-fallback" style={{ background: colorFor(user?.id ?? name), fontSize: size * 0.42 }}>
          {[...name][0]?.toUpperCase()}
        </div>
      )}
      {status && <span className={"status-dot " + status} style={{ width: size * 0.32, height: size * 0.32 }} />}
    </div>
  );
}

export function GuildIcon({ name, icon, size = 48 }: { name: string; icon: string; size?: number }) {
  if (icon) return <img className="guild-icon-img" src={icon} alt="" style={{ width: size, height: size }} />;
  const initials = name
    .split(/\s+/)
    .map((w) => [...w][0])
    .join("")
    .slice(0, 3);
  return (
    <div className="guild-icon-text" style={{ width: size, height: size, fontSize: initials.length > 2 ? size * 0.28 : size * 0.36 }}>
      {initials}
    </div>
  );
}

export function Modal({ title, onClose, children, wide, footer, className }: { title?: ReactNode; onClose: () => void; children: ReactNode; wide?: boolean; footer?: ReactNode; className?: string }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [onClose]);
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={"modal" + (wide ? " wide" : "") + (className ? " " + className : "")} role="dialog">
        {title && (
          <div className="modal-head">
            <h2>{title}</h2>
            <button className="icon-btn" onClick={onClose} aria-label="Close">
              <Icon name="close" />
            </button>
          </div>
        )}
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </div>
  );
}

export const closeModal = () => set({ modal: null });

export function openMenu(e: { clientX: number; clientY: number; preventDefault?: () => void; stopPropagation?: () => void }, items: ContextMenuItem[]) {
  e.preventDefault?.();
  e.stopPropagation?.();
  set({ menu: { x: e.clientX, y: e.clientY, items } });
}

export function ContextMenu() {
  const menu = useStore((s) => s.menu);
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ x: 0, y: 0 });
  useLayoutEffect(() => {
    if (!menu || !ref.current) return;
    const r = ref.current.getBoundingClientRect();
    setPos({ x: Math.max(8, Math.min(menu.x, innerWidth - r.width - 8)), y: Math.max(8, Math.min(menu.y, innerHeight - r.height - 8)) });
  }, [menu]);
  useEffect(() => {
    if (!menu) return;
    const close = (e: Event) => {
      if (ref.current && e.target instanceof Node && ref.current.contains(e.target)) return;
      set({ menu: null });
    };
    const esc = (e: KeyboardEvent) => e.key === "Escape" && set({ menu: null });
    setTimeout(() => {
      window.addEventListener("mousedown", close);
      window.addEventListener("touchstart", close);
    });
    window.addEventListener("keydown", esc);
    window.addEventListener("blur", close);
    return () => {
      window.removeEventListener("mousedown", close);
      window.removeEventListener("touchstart", close);
      window.removeEventListener("keydown", esc);
      window.removeEventListener("blur", close);
    };
  }, [menu]);
  if (!menu) return null;
  return (
    <div className="context-menu" ref={ref} style={{ left: pos.x, top: pos.y }}>
      {menu.items.map((it, i) =>
        it.divider ? (
          <div key={i} className="menu-divider" />
        ) : it.slider ? (
          <MenuSlider key={i} item={it} />
        ) : (
          <button
            key={i}
            className={"menu-item" + (it.danger ? " danger" : "")}
            disabled={it.disabled}
            onClick={() => {
              set({ menu: null });
              it.onClick?.();
            }}
          >
            <span>{it.label}</span>
            {it.checked !== undefined && <span className={"checkbox" + (it.checked ? " on" : "")}>{it.checked && <Icon name="check" size={14} />}</span>}
          </button>
        ),
      )}
    </div>
  );
}

function MenuSlider({ item }: { item: ContextMenuItem }) {
  const s = item.slider!;
  const [v, setV] = useState(s.value);
  return (
    <div className="menu-slider">
      <div className="menu-slider-label">
        <span>{item.label}</span>
        <span>{s.format ? s.format(v) : v}</span>
      </div>
      <input
        type="range"
        min={s.min}
        max={s.max}
        value={v}
        onChange={(e) => {
          const n = Number(e.target.value);
          setV(n);
          s.onChange(n);
        }}
      />
    </div>
  );
}

export function Toast() {
  const toast = useStore((s) => s.toast);
  if (!toast) return null;
  return (
    <div className="toast" key={toast.id} onClick={() => set({ toast: null })}>
      {toast.text}
    </div>
  );
}

// A password box with an eye button to show what you typed.
export function PasswordInput(props: Omit<React.InputHTMLAttributes<HTMLInputElement>, "type">) {
  const [shown, setShown] = useState(false);
  return (
    <div className="password-box">
      <input {...props} type={shown ? "text" : "password"} />
      <button type="button" className="icon-btn tiny" title={shown ? "Hide password" : "Show password"} aria-label={shown ? "Hide password" : "Show password"} onClick={() => setShown(!shown)}>
        <Icon name={shown ? "eyeOff" : "eye"} size={18} />
      </button>
    </div>
  );
}

export function Toggle({ on, onChange, disabled }: { on: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button type="button" role="switch" aria-checked={on} disabled={disabled} className={"toggle" + (on ? " on" : "")} onClick={() => onChange(!on)}>
      <span />
    </button>
  );
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: ReactNode }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function useNow(ms = 1000) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}

export function formatTime(id: ID | number): string {
  const t = typeof id === "number" ? id : idTime(id);
  const d = new Date(t);
  const now = new Date();
  const time = d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  if (d.toDateString() === now.toDateString()) return `Today at ${time}`;
  const y = new Date(now);
  y.setDate(now.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return `Yesterday at ${time}`;
  return d.toLocaleDateString([], { month: "numeric", day: "numeric", year: "numeric" }) + " " + time;
}

const EPOCH = 1767225600000n;
export function idTime(id: ID): number {
  return Number((BigInt(id) >> 22n) + EPOCH);
}

export function formatBytes(n: number) {
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
  if (n < 1024 ** 3) return (n / 1024 / 1024).toFixed(1) + " MB";
  return (n / 1024 ** 3).toFixed(2) + " GB";
}

export function hexColor(n: number) {
  return n ? "#" + n.toString(16).padStart(6, "0") : undefined;
}
