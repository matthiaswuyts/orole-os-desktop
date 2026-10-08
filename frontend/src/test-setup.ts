import "@testing-library/jest-dom/vitest"

// jsdom does not implement Web Animations; it cannot create running animations.
// Base UI queries this standard DOM API when settling scroll-area layout.
if (!Element.prototype.getAnimations) {
  Element.prototype.getAnimations = () => [];
}
