import { useState } from "react";

export interface Props {
  name: string;
}

export function Greeting(props: Props) {
  const [count, setCount] = useState(0);
  return (
    <div onClick={() => setCount(count + 1)}>
      {props.name}: {count}
    </div>
  );
}

export const Title = () => <h1>Hello</h1>;
