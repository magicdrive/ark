export interface Service {
  create(name: string): unknown;
}

export class BaseService {
  protected log(message: string): void {}
}
