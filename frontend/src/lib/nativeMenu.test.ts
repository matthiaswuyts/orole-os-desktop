import { afterEach, describe, expect, it } from "vitest";
import { disableNativeContextMenu } from "./nativeMenu";

const stop = disableNativeContextMenu();
afterEach(() => { document.body.innerHTML = ""; });

/** Fires a contextmenu event and says whether the browser would still have opened its menu. */
function nativeMenuOpens(el: Element): boolean {
  const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
  el.dispatchEvent(event);
  return !event.defaultPrevented;
}

describe("native context menu", () => {
  it("is suppressed on ordinary content, links and images", () => {
    document.body.innerHTML = `<main><p id="p">text</p><a id="a" href="#x">link</a><img id="i" alt=""></main>`;
    for (const id of ["p", "a", "i"]) expect(nativeMenuOpens(document.getElementById(id)!)).toBe(false);
  });

  it("stays on editable text fields so paste and spell-check work", () => {
    document.body.innerHTML = `<input id="t" type="text"><textarea id="ta"></textarea><div id="ce" contenteditable="true"><b id="inner">x</b></div><input id="cb" type="checkbox"><div id="off" contenteditable="false">n</div>`;
    expect(nativeMenuOpens(document.getElementById("t")!)).toBe(true);
    expect(nativeMenuOpens(document.getElementById("ta")!)).toBe(true);
    expect(nativeMenuOpens(document.getElementById("inner")!)).toBe(true);
    expect(nativeMenuOpens(document.getElementById("cb")!)).toBe(false);
    expect(nativeMenuOpens(document.getElementById("off")!)).toBe(false);
  });

  it("can be removed", () => {
    stop();
    document.body.innerHTML = `<p id="p">text</p>`;
    expect(nativeMenuOpens(document.getElementById("p")!)).toBe(true);
  });
});
