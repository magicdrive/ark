class Store:
    def put(self, item):
        self.items().append(item)

    def items(self):
        return []
