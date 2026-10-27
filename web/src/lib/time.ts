import { t } from "./i18n";

// 90 -> "1h 30m", 120 -> "2h", 45 -> "45m".
export function formatMinutes(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return t("time.m", { m });
  return m === 0 ? t("time.h", { h }) : t("time.hm", { h, m });
}
