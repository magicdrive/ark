import { UserRepository } from "../repo/user-repository";
import { User as DomainUser } from "../domain";
import { BaseService, Service } from "./base";

export class UserService extends BaseService implements Service {
  constructor(private readonly repo: UserRepository) {
    super();
  }

  create(name: string): DomainUser {
    const u = new DomainUser(name);
    this.repo.save(u);
    this.audit(name);
    return u;
  }

  private audit(name: string): void {
    this.log(name);
  }
}

// Same-file noise: unrelated to UserService.create.
export class UserServiceMetrics {
  record(): void {}
}
