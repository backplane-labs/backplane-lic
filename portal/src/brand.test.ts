import { describe, expect, it } from 'vitest';
import { applyBrand, contrastText } from './brand';

describe('brand', () => {
  it('picks readable text for the accent', () => {
    expect(contrastText('#ffd400')).toBe('#0b1220');
    expect(contrastText('#1d3bb8')).toBe('#ffffff');
  });
  it('sets the accent variables and ignores invalid colors', () => {
    const root = document.createElement('div');
    applyBrand('#1d3bb8', root);
    expect(root.style.getPropertyValue('--primary')).toBe('#1d3bb8');
    expect(root.style.getPropertyValue('--primary-foreground')).toBe('#ffffff');
    const other = document.createElement('div');
    applyBrand('red; background: url(x)', other);
    expect(other.style.getPropertyValue('--primary')).toBe('');
  });
});
