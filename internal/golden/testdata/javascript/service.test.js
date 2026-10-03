import { UserService } from "./service.js";

test("create", () => {
  const s = new UserService();
  s.create("alice");
});
