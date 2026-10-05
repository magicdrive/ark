import { Account, Order } from "./index";

export function run(): void {
  const a = new Account();
  a.open();
  new Order();
}
