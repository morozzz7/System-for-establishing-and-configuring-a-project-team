/**
 * Vitest/jsdom setup: browser APIs that Ant Design needs but jsdom does not implement.
 */
import '@testing-library/jest-dom';

if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }),
  });
}

// jsdom throws "Not implemented" when a pseudo-element argument is passed (rc-util does this
// when measuring scrollbars); ignore the second argument so Drawer/Modal tests stay quiet.
if (typeof window !== 'undefined' && typeof window.getComputedStyle === 'function') {
  const original = window.getComputedStyle.bind(window);
  window.getComputedStyle = ((element: Element) => original(element)) as typeof window.getComputedStyle;
}

if (typeof window !== 'undefined' && typeof window.ResizeObserver !== 'function') {
  class ResizeObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  window.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
}
