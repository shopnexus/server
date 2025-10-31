import { PrismaClient, Prisma } from '@prisma/client'
import slugify from '@sindresorhus/slugify'
import { v4 as uuidv4 } from 'uuid'
import fs from "fs"

const prisma = new PrismaClient()

type InputProduct1 = {
  title?: string
  brand?: string
  breadcrumb?: string[]
  final_price?: number | string
  initial_price?: number | string
  sold?: number | string
  image?: string[]
  [k: string]: any
}

type InputProduct2 = {
  main_category: string
  title: string
  average_rating: number
  rating_number: number
  features: string[]
  description: string[]
  price: number
  images: {
    thumb: string
    large: string
    variant: string
    hi_res: string
  }[]
  videos: string[]
  store: string
  categories: string[]
  details: Record<string, any>
  parent_asin: string
  bought_together: unknown | null
}

function toBigInt(value: any, fallback: bigint = BigInt(0)): bigint {
  if (value === null || value === undefined || value === '') return fallback
  const n = typeof value === 'string' ? Number(value.replace(/[^0-9.-]/g, '')) : Number(value)
  if (!Number.isFinite(n)) return fallback
  try {
    return BigInt(Math.trunc(n))
  } catch {
    return fallback
  }
}

function pickCurrentStock(input: any): bigint {
  // Prefer explicit numeric fields
  if (input.stock && String(input.stock).trim() !== '') return toBigInt(input.stock)
  // Look into Product Specifications
  const specs: Array<{ name: string; value: any }> = input["Product Specifications"] || []
  const found = specs.find(s => /existencias|stock/i.test(s.name || ''))
  if (found) return toBigInt(found.value)
  return BigInt(0)
}

function mapAttributes(input: any): Array<{ name: string; value: string }> {
  const variations = Array.isArray(input.variations) ? input.variations : []
  const chosen = Array.isArray(input.product_variation) ? input.product_variation : []
  if (variations.length && chosen.length && typeof chosen[0]?.value === 'string') {
    const chosenValues = String(chosen[0].value).split(',').map((s: string) => s.trim())
    return variations.map((v: any, idx: number) => ({
      name: v.name || `Option ${idx + 1}`,
      value: chosenValues[idx] || (Array.isArray(v.variations) ? v.variations[0] : '')
    }))
  }
  // Fallback: take first option of each variation
  if (variations.length) {
    return variations.map((v: any, idx: number) => ({
      name: v.name || `Option ${idx + 1}`,
      value: Array.isArray(v.variations) && v.variations.length ? String(v.variations[0]) : ''
    }))
  }
  return []
}

async function upsertBrand(brandName?: string): Promise<bigint> {
  const name = (brandName && String(brandName).trim()) || 'Unknown'
  const code = slugify(name)
  const brand = await prisma.brand.upsert({
    where: { code },
    update: { name },
    create: { code, name, description: name },
  })
  return BigInt(brand.id)
}

async function upsertCategoryFromBreadcrumb(breadcrumb?: string[]): Promise<bigint> {
  // Use the last breadcrumb element as category to avoid uniqueness conflicts across hierarchy
  const leaf = Array.isArray(breadcrumb) && breadcrumb.length ? breadcrumb[breadcrumb.length - 1] : 'General'
  const name = String(leaf || 'General')
  const existed = await prisma.productCategory.findUnique({ where: { name } })
  if (existed) return BigInt(existed.id)
  const created = await prisma.productCategory.create({ data: { name, description: name, parent_id: null } })
  return BigInt(created.id)
}

async function main(inputs: InputProduct1[] = []) {
  const account = await prisma.account.create({
    data: {
      type: 'Vendor',
      email: `vendor+${uuidv4()}@example.com`,
      password: 'password123',
      vendor_profile: {
        create: {

        }
      }
    }
  })

  for (const input of inputs) {
    const brandId = await upsertBrand(input.brand)
    const categoryId = await upsertCategoryFromBreadcrumb(input.breadcrumb)

    const spu = await prisma.productSpu.create({
      data: {
        account_id: account.id,
        code: slugify(input.title || 'product') + "." + uuidv4(),
        category_id: categoryId,
        brand_id: brandId,
        name: input.title || 'Unnamed Product',
        description: (input["Product Description"] || input.description || '').toString(),
        is_active: String(input.is_available || '').toLowerCase() !== 'false',
        featured_sku_id: null, // to be updated later

        products: {
          create: {
            price: toBigInt(input.final_price ?? input.initial_price),
            can_combine: false,
            date_created: new Date(),
            // Prisma schema now stores attributes directly as Json on ProductSku
            attributes: mapAttributes(input),
          }
        },
      },
      include: { products: true }
    })

    const product = spu.products[0]

    // Update featured_sku_id
    await prisma.productSpu.update({
      where: { id: spu.id },
      data: { featured_sku_id: product.id }
    })

    await prisma.stock.upsert({
      where: { ref_id_ref_type: { ref_id: product.id, ref_type: 'ProductSku' } },
      update: {
        current_stock: pickCurrentStock(input),
        sold: toBigInt(input.sold),
      },
      create: {
        ref_id: product.id,
        ref_type: 'ProductSku',
        current_stock: pickCurrentStock(input),
        sold: toBigInt(input.sold),
        date_created: new Date(),
      }
    })

    const images: string[] = Array.isArray(input.image) ? input.image.slice(0, 10) : []
    let order = 0
    for (const url of images) {
      if (!url || typeof url !== 'string') continue
      await prisma.resourceReference.create({
        data: {
          ref_id: spu.id,
          ref_type: 'ProductSpu',
          is_primary: order === 0,
          order: order++,
          resource: {
            connectOrCreate: {
              create: {
                id: uuidv4(),
                provider: 'remote',
                metadata: Prisma.JsonNull,
                object_key: url,
                uploaded_by: account.id,
                status: 'Success',
                mime: url.includes('.png') ? 'image/png' : 'image/jpeg',
                size: BigInt(0),
              },
              where: {
                provider_object_key: {
                  provider: 'remote',
                  object_key: url,
                }
              }
            }
          }
        }
      })
    }

    await prisma.searchSync.create({
      data: {
        ref_type: 'Product',
        ref_id: spu.id,
        is_stale_embedding: true,
        is_stale_metadata: true,
        date_created: new Date(),
      }
    })
  }
}

const inputs: InputProduct1[] = JSON.parse(fs.readFileSync('data.json', 'utf-8'))

main(inputs)
  .catch(e => console.error(e))
  .finally(async () => {
    await prisma.$disconnect()
  })