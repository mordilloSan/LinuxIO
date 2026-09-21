export const frequencies = {
  secondly: "Every second",
  minutely: "Every minute",
  hourly: "Hourly",
  daily: "Daily",
  weekly: "Weekly",
  custom: "Custom (advanced)",
} as const;

export const weekdays = {
  Mon: "Monday",
  Tue: "Tuesday",
  Wed: "Wednesday",
  Thu: "Thursday",
  Fri: "Friday",
  Sat: "Saturday",
  Sun: "Sunday",
} as const;

export interface ScheduleTiming {
  frequency: keyof typeof frequencies;
  time: string;
  weekday: keyof typeof weekdays;
  custom: string;
}

export function parseScheduleTiming(calendar: string): ScheduleTiming {
  const value = calendar.trim();
  const timing: ScheduleTiming = {
    frequency: "custom",
    time: "03:00",
    weekday: "Mon",
    custom: calendar,
  };
  const aliases: Record<string, ScheduleTiming["frequency"]> = {
    secondly: "secondly",
    "*-*-* *:*:*": "secondly",
    minutely: "minutely",
    "*-*-* *:*:00": "minutely",
    hourly: "hourly",
    "*-*-* *:00:00": "hourly",
    daily: "daily",
    weekly: "weekly",
  };
  if (Object.hasOwn(aliases, value)) {
    return { ...timing, frequency: aliases[value], time: "00:00" };
  }
  const match =
    /^(?:(Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+)?(?:\*-\*-\*\s+)?([01]\d|2[0-3]):([0-5]\d)(?::00)?$/.exec(
      value,
    );
  if (!match) return timing;
  return {
    ...timing,
    frequency: match[1] ? "weekly" : "daily",
    weekday: (match[1] as ScheduleTiming["weekday"]) ?? "Mon",
    time: `${match[2]}:${match[3]}`,
  };
}

export function scheduleCalendar(timing: ScheduleTiming): string {
  switch (timing.frequency) {
    case "secondly":
      return "*-*-* *:*:*";
    case "minutely":
      return "*-*-* *:*:00";
    case "hourly":
      return "hourly";
    case "daily":
    case "weekly":
      if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(timing.time)) return "";
      return `${timing.frequency === "weekly" ? `${timing.weekday} ` : ""}*-*-* ${timing.time}:00`;
    case "custom":
      return timing.custom;
  }
}

export function scheduleSummary(calendar: string): string {
  const timing = parseScheduleTiming(calendar);
  if (timing.frequency === "custom") return calendar;
  if (timing.frequency !== "daily" && timing.frequency !== "weekly")
    return frequencies[timing.frequency];
  const time = new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: "UTC",
  }).format(new Date(`2000-01-01T${timing.time}:00Z`));
  return `${timing.frequency === "weekly" ? `Every ${weekdays[timing.weekday]}` : "Daily"} at ${time}`;
}
