import { Repository } from "./repository";
import { User } from "./user";

export class UserService {
  private repo = new Repository();

  create(name: string): void {
    const u = new User();
    u.name = name;
    const found = this.repo.find(u.id);
    found.save();
  }
}
