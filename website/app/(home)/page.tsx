import {
  ArrowRight,
  Cpu,
  Github,
  Layers,
  Settings,
  Shield,
  Terminal,
  Zap,
} from "lucide-react";
import Link from "next/link";

export default function HomePage() {
  return (
    <main className="flex flex-col min-h-screen bg-fd-background text-fd-foreground overflow-hidden selection:bg-fd-primary/20">
      <section className="relative flex flex-col items-center justify-center px-6 py-24 text-center overflow-hidden lg:py-32">
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[500px] bg-fd-primary/10 blur-[100px] rounded-full pointer-events-none" />

        <div className="relative z-10 max-w-4xl space-y-8">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-fd-secondary/50 border border-fd-border text-xs font-medium text-fd-muted-foreground mb-4">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-green-500" />
            </span>
            v0.1.0 Now Available
          </div>

          <h1 className="text-4xl md:text-6xl lg:text-7xl font-bold tracking-tight bg-clip-text text-transparent bg-gradient-to-b from-fd-foreground to-fd-foreground/60">
            Eliminate Stutter.
            <br />
            <span className="text-fd-primary">Maximize FPS.</span>
          </h1>

          <p className="text-lg md:text-xl text-fd-muted-foreground max-w-2xl mx-auto leading-relaxed">
            Intelligent CPU affinity management for Linux gamers on AMD Ryzen 9
            and Threadripper. Automatically isolates games to dedicated CCDs for
            butter-smooth performance.
          </p>

          <div className="flex flex-col sm:flex-row items-center justify-center gap-4 pt-4">
            <Link
              href="/docs"
              className="group relative inline-flex h-11 items-center justify-center gap-2 rounded-md bg-fd-primary px-8 text-sm font-medium text-fd-primary-foreground transition-all hover:bg-fd-primary/90 hover:ring-2 hover:ring-fd-primary/20 hover:ring-offset-2 hover:ring-offset-fd-background"
            >
              Get Started
              <ArrowRight className="size-4 transition-transform group-hover:translate-x-1" />
            </Link>
            <Link
              href="https://github.com/youruser/quicksetd"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex h-11 items-center justify-center gap-2 rounded-md border border-fd-border bg-fd-secondary px-8 text-sm font-medium transition-colors hover:bg-fd-secondary/80 hover:text-fd-foreground"
            >
              <Github className="size-4" />
              GitHub
            </Link>
          </div>
        </div>
      </section>

      <section className="px-6 pb-20">
        <div className="max-w-3xl mx-auto">
          <div className="rounded-xl border border-fd-border bg-fd-card shadow-2xl overflow-hidden">
            <div className="flex items-center gap-2 px-4 py-3 border-b border-fd-border bg-fd-muted/50">
              <div className="flex gap-1.5">
                <div className="w-3 h-3 rounded-full bg-red-500/20 border border-red-500/50" />
                <div className="w-3 h-3 rounded-full bg-yellow-500/20 border border-yellow-500/50" />
                <div className="w-3 h-3 rounded-full bg-green-500/20 border border-green-500/50" />
              </div>
              <div className="text-xs text-fd-muted-foreground font-mono ml-2">
                bash
              </div>
            </div>
            <div className="p-6 overflow-x-auto bg-[#0d0d0d]">
              <pre className="font-mono text-sm leading-relaxed">
                <code className="text-gray-300">
                  <span className="text-fd-muted-foreground">
                    # Install ccdbind
                  </span>
                  {"\n"}
                  <span className="text-blue-400">go</span> install
                  github.com/youruser/quicksetd/cmd/ccdbind@latest
                  {"\n\n"}
                  <span className="text-fd-muted-foreground">
                    # Enable the service
                  </span>
                  {"\n"}
                  <span className="text-blue-400">systemctl</span> --user enable
                  --now ccdbind
                  {"\n\n"}
                  <span className="text-fd-muted-foreground">
                    # Check status
                  </span>
                  {"\n"}
                  <span className="text-blue-400">ccdbind</span> status
                </code>
              </pre>
            </div>
          </div>
        </div>
      </section>

      <section className="px-6 py-20 bg-fd-card/30 border-t border-fd-border/50">
        <div className="max-w-6xl mx-auto space-y-12">
          <div className="text-center max-w-2xl mx-auto space-y-4">
            <h2 className="text-3xl font-bold">Built for Performance</h2>
            <p className="text-fd-muted-foreground">
              Designed to work silently in the background, ensuring your games
              get exclusive access to your best cores.
            </p>
          </div>

          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
            <FeatureCard
              icon={<Cpu className="size-6 text-blue-400" />}
              title="Topology Aware"
              description="Automatically detects CCD and L3 cache groups on AMD processors to optimize thread placement."
            />
            <FeatureCard
              icon={<Layers className="size-6 text-purple-400" />}
              title="Process Isolation"
              description="Moves game processes to a dedicated systemd slice, keeping background tasks away from your game."
            />
            <FeatureCard
              icon={<Zap className="size-6 text-yellow-400" />}
              title="Zero Config"
              description="Smart defaults for most Ryzen 9 and Threadripper setups. Just install and play."
            />
            <FeatureCard
              icon={<Terminal className="size-6 text-green-400" />}
              title="Systemd Native"
              description="Integrates deeply with systemd for robust resource management and process tracking."
            />
            <FeatureCard
              icon={<Settings className="size-6 text-orange-400" />}
              title="Steam Integrated"
              description="Detects Steam and Proton games automatically. Use launch options for fine-grained control."
            />
            <FeatureCard
              icon={<Shield className="size-6 text-cyan-400" />}
              title="Resource Guard"
              description="Pins background apps to OS cores, preventing them from stealing cycles from your game."
            />
          </div>
        </div>
      </section>

      <section className="px-6 py-24 text-center">
        <h2 className="text-2xl font-bold mb-6">Ready to optimize your rig?</h2>
        <Link
          href="/docs"
          className="inline-flex h-11 items-center justify-center gap-2 rounded-md bg-fd-foreground px-8 text-sm font-medium text-fd-background transition-opacity hover:opacity-90"
        >
          Read the Docs
        </Link>
      </section>
    </main>
  );
}

function FeatureCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div className="group p-6 rounded-xl bg-fd-card border border-fd-border transition-all hover:border-fd-primary/30 hover:shadow-lg hover:shadow-fd-primary/5">
      <div className="mb-4 inline-flex p-3 rounded-lg bg-fd-muted/50 group-hover:bg-fd-muted transition-colors">
        {icon}
      </div>
      <h3 className="text-lg font-semibold mb-2">{title}</h3>
      <p className="text-sm text-fd-muted-foreground leading-relaxed">
        {description}
      </p>
    </div>
  );
}
