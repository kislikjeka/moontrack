import { Menu, Moon } from 'lucide-react'
import { useSidebar } from '@/hooks/useSidebar'
import { ThemeToggle } from './ThemeToggle'
import { Button } from '@/components/ui/button'

export function Header() {
  const { setMobileOpen } = useSidebar()

  return (
    <header className="sticky top-0 z-40 h-16 border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex h-full items-center justify-between px-4">
        {/* Mobile menu trigger */}
        <div className="flex items-center gap-2 md:hidden">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setMobileOpen(true)}
          >
            <Menu className="h-5 w-5" />
            <span className="sr-only">Open menu</span>
          </Button>
          <div className="flex items-center gap-2">
            <Moon className="h-5 w-5 text-primary" />
            <span className="font-semibold">MoonTrack</span>
          </div>
        </div>

        {/* Desktop: empty space where breadcrumbs could go */}
        <div className="hidden md:block" />

        {/* Right side */}
        <div className="flex items-center gap-2">
          <ThemeToggle />
        </div>
      </div>
    </header>
  )
}
