import { User } from "../domain";

export class UserRepository {
  save(u: User): void {
    // persist the user
  }

  find(id: string): User | undefined {
    return undefined;
  }
}
