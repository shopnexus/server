import { faker } from "@faker-js/faker";
import { PrismaClient, Prisma } from "@prisma/client";
import { v4 } from "uuid";
import { uuid } from "uuidv4";

const prisma = new PrismaClient();

async function main() {
  const skus = await prisma.productSku.findMany({});
  console.log(`Found ${skus.length} SKUs in the database.`);

  await prisma.productSerial.createMany({
    data: skus
      .map((sku) =>
        Array.from({ length: 10 }).map((a) => ({
          serial_id: v4(),
          sku_id: sku.id,
        })),
      )
      .flat(),
    skipDuplicates: true,
  });
}

main()
  .catch((e) => console.error(e))
  .finally(async () => {
    await prisma.$disconnect();
  });
