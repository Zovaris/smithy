import { describe, expect, it } from "vitest";
import { route, ROUTES } from "./router";

describe("route", () => {
  it("maps a hash to a known screen", () => {
    expect(route("#/decisions")).toBe("decisions");
    expect(route("#/board")).toBe("board");
  });
  it("defaults to sessions for empty or unknown hashes", () => {
    expect(route("")).toBe("sessions");
    expect(route("#/nope")).toBe("sessions");
  });
  it("lists the four screens in nav order", () => {
    expect(ROUTES.map((r) => r.id)).toEqual(["sessions", "decisions", "agents", "board"]);
  });
});
