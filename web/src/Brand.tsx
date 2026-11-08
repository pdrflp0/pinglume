export function Mark({ size = 28, live = false }: { size?: number; live?: boolean }) {
  return (
    <svg className={live ? "mark mark-live" : "mark"} width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <circle className="mark-ring" cx="22" cy="10" r="6.5" />
      <circle className="mark-core" cx="22" cy="10" r="3.2" />
      <path className="mark-plume" d="M19.4 12.6C12.5 14.4 7.2 19.6 4.5 28.5C9.8 23.6 15.2 21 21 16.4Z" />
    </svg>
  );
}
