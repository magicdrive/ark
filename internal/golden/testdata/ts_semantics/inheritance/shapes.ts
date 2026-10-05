export interface Parent {
  id(): string;
}

export interface Child extends Parent {
  extra(): void;
}

export class Base {}

export class Impl extends Base implements Child {
  id(): string {
    return "x";
  }
  extra(): void {}
}
