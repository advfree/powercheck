import { Desktop, Moon, Sun } from "@phosphor-icons/react";

export const themeOptions = [
  { value: "system", label: "跟随系统", icon: Desktop },
  { value: "light", label: "亮色", icon: Sun },
  { value: "dark", label: "深色", icon: Moon },
];

export function ThemeControl({ preference, onChange, compact = false }) {
  const selected = themeOptions.find((option) => option.value === preference) ?? themeOptions[0];
  const Icon = selected.icon;
  return (
    <label className={`theme-control ${compact ? "theme-control--compact" : ""}`}>
      <Icon size={18} weight="duotone" />
      <span className="sr-only">界面主题</span>
      <select
        value={preference}
        onChange={(event) => onChange(event.target.value)}
        aria-label="界面主题"
      >
        {themeOptions.map((option) => (
          <option value={option.value} key={option.value}>{option.label}</option>
        ))}
      </select>
    </label>
  );
}
