import { User } from "./user";

export class Repository {
  find(id: number): User {
    const u = new User();
    u.id = id;
    return u;
  }
}
