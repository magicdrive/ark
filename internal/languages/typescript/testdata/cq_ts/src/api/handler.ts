import { UserRepository } from "../repo/user-repository";

export function typedHandler(repo: UserRepository, u: any) {
  repo.save(u);
}

export function untypedHandler(repo: any, u: any) {
  repo.save(u);
}

// `find` is unique in the repository, but an untyped receiver is still no evidence.
export function untypedFind(repo: any) {
  return repo.find("id");
}
