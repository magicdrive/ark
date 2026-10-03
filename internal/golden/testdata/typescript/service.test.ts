import { UserService } from "./service";

test("create", () => {
  const s = new UserService();
  s.create("alice");
});
