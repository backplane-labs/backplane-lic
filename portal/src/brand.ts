// applyBrand paints the vendor's accent over both themes. The text on the
// accent switches between dark and light by the accent's luminance so
// primary buttons stay readable whatever color the vendor picks.
export function applyBrand(accent: string | undefined, root: HTMLElement = document.documentElement): void {
  if (!accent || !/^#[0-9a-fA-F]{6}$/.test(accent)) return;
  root.style.setProperty('--primary', accent);
  root.style.setProperty('--ring', accent);
  root.style.setProperty('--link', accent);
  root.style.setProperty('--primary-foreground', contrastText(accent));
}

export function contrastText(accent: string): string {
  const channel = (offset: number) => {
    const value = parseInt(accent.slice(offset, offset + 2), 16) / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  };
  const luminance = 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
  return luminance > 0.4 ? '#0b1220' : '#ffffff';
}
