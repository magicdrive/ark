import { User } from "./user.js";

export class Repository {
  find(id) {
    const u = new User();
    u.id = id;
    return u;
  }
}
