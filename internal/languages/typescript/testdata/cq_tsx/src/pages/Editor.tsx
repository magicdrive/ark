import { UserStore as Store } from "../data/user-store";

export function saveUser(store: Store) {
  store.save();
  return <span />;
}

export function saveUnknown(store: any) {
  store.save();
  return <i />;
}
