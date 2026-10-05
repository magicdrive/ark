import { UserRepository } from "../ambiguity/repositories";
import { User } from "../imports/user";

export abstract class BaseService {
  abstract name(): string;
  protected log(msg: string): void {}
}

export class UserService extends BaseService {
  private count = 0;

  constructor(private readonly repo: UserRepository) {
    super();
  }

  name(): string {
    return "users";
  }

  create(name: string): User {
    const u = new User(name);
    this.repo.save(u);
    this.log("created");
    return u;
  }

  static from(repo: UserRepository): UserService {
    return new UserService(repo);
  }

  get total(): number {
    return this.count;
  }

  set total(v: number) {
    this.count = v;
  }
}

export interface Service {
  name(): string;
  readonly id: string;
}
