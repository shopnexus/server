import { faker } from "@faker-js/faker";
import { PrismaClient, Prisma } from "@prisma/client";

const prisma = new PrismaClient();

async function main() {
  const spus = await prisma.productSpu.findMany({});
  await prisma.comment.createMany({
    data: spus
      .map((spu) =>
        Array.from({ length: faker.number.int({ min: 5, max: 10 }) }).map(
          () =>
            ({
              account_id: 1,
              body: faker.lorem.paragraph(),
              ref_id: spu.id,
              ref_type: "ProductSpu",
              score: faker.number.int({ min: 1, max: 100 }),
              upvote: faker.number.int({ min: 1, max: 100 }),
            }) as Prisma.CommentCreateManyInput,
        ),
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
