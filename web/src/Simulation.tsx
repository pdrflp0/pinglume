import { useEffect, useState } from "react";
import { api } from "./api";

export function SimulationBanner() {
  const [demo, setDemo] = useState(false);
  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .mode()
        .then((m) => alive && setDemo(m.mode === "demo"))
        .catch(() => {});
    load();
    const t = setInterval(load, 30_000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);
  useEffect(() => {
    const base = "Pinglume";
    document.title = demo ? `[SIMULAÇÃO] ${base}` : base;
    document.documentElement.dataset.demo = demo ? "1" : "";
  }, [demo]);
  if (!demo) return null;
  return (
    <div className="simulation-banner" role="note">
      SIMULAÇÃO — modo demonstração: todos os dados desta tela são gerados, não são medições reais
    </div>
  );
}
