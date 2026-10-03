import { Repository } from "./repository.js";
import { User } from "./user.js";

export class UserService {
  constructor() {
    this.repo = new Repository();
  }

  create(name) {
    const u = new User();
    u.name = name;
    const found = this.repo.find(u.id);
    found.save();
  }
}
