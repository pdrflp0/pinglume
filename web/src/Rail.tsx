import { useEffect, useState } from "react";
import { Mark } from "./Brand";

const ITEMS = [
  { id: "visao", label: "Visão geral" },
  { id: "tempo", label: "Linha do tempo" },
  { id: "relatos", label: "Relatos" },
  { id: "comparacao", label: "Comparação" },
  { id: "cobertura", label: "Cobertura" },
  { id: "exportar", label: "Exportar" },
  { id: "sondas", label: "Sondas" },
];

export function Rail({ measuring }: { measuring: boolean }) {
  const [active, setActive] = useState(ITEMS[0].id);

  useEffect(() => {
    let frame = 0;
    const update = () => {
      frame = 0;
      const line = window.innerHeight * 0.3;
      let current = ITEMS[0].id;
      for (const it of ITEMS) {
        const el = document.getElementById(it.id);
        if (el && el.getBoundingClientRect().top <= line) current = it.id;
      }
      if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 24) {
        current = ITEMS[ITEMS.length - 1].id;
      }
      setActive(current);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  return (
    <aside className="rail">
      <div className="rail-brand">
        <Mark size={30} live={measuring} />
        <span className="wordmark">pinglume</span>
      </div>
      <nav className="rail-nav" aria-label="Seções do painel">
        {ITEMS.map((it, i) => (
          <a key={it.id} href={`#${it.id}`} className={active === it.id ? "on" : undefined} aria-current={active === it.id ? "location" : undefined}>
            <span className="rail-n">{String(i).padStart(2, "0")}</span>
            {it.label}
          </a>
        ))}
      </nav>
      <p className="rail-note">Os dados ficam somente neste computador.</p>
    </aside>
  );
}
