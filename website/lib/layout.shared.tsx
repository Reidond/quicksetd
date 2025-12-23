import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { Cpu } from "lucide-react";

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <>
          <Cpu className="size-5" />
          <span className="font-semibold">ccdbind</span>
        </>
      ),
    },
    links: [
      {
        text: "Documentation",
        url: "/docs",
        active: "nested-url",
      },
      {
        text: "GitHub",
        url: "https://github.com/youruser/quicksetd",
        external: true,
      },
    ],
    githubUrl: "https://github.com/youruser/quicksetd",
  };
}
