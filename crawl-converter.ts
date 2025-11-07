import { PrismaClient, Prisma } from "@prisma/client"
import slugify from "@sindresorhus/slugify"
import { v4 as uuidv4 } from "uuid"
import fs from "fs"
import cliProgress from "cli-progress"
import translate from "google-translate-api-x"

const prisma = new PrismaClient()

// Translation function - tries to use google-translate-api-x if available
async function translateToEnglish(text: string): Promise<string> {
	if (!text || typeof text !== "string") return text
	
	// Check if text is already in English (basic check - mostly ASCII)
	const isEnglish = /^[\x00-\x7F]*$/.test(text) || text.trim().length === 0
	if (isEnglish) return text

	try {
		const result = await translate(text, { to: "en" })
		return result.text || text
	} catch (error) {
		// If translation library is not available, return original text
		console.warn(`Translation failed, using original text: ${text.substring(0, 50)}...`)
		return text
	}
}

type InputProduct1 = {
	url: string
	id: number
	title: string
	sold: number
	rating: number
	reviews: number
	initial_price: number
	final_price: number
	currency: string
	stock: string
	favorite: number
	image: string[]
	video: string | null
	seller_name: string
	shop_url: string
	breadcrumb: string[]
	"Product Specifications": {
		name: string
		value: string
	}[]
	"Product Description": string
	seller_rating: number
	seller_products: number
	seller_chats_responded_percentage: number
	seller_chat_time_reply: string
	seller_joined_date: string
	seller_followers: number
	variations: {
		name: string
		variations: string[]
	}[]
	domain: string
	brand: string
	category_id: number
	flash_sale: boolean
	flash_sale_time: string
	product_variation: {
		name: string
		value: string
	}[]
	gmv_cal: number
	category_url: string
	vouchers:
		| {
				claimable: boolean
				currency: string
				discount: number | null
				discount_cap: number
				discount_percentage: number
				min_spend: number
				shop_vouncher: string
				special_condition: string | null
				text_info: string
				validity_end: string
				validity_start: string
		  }[]
		| null
	is_available: string
	seller_id: string
	product_ratings: unknown
}


function toBigInt(value: any, fallback: bigint = BigInt(0)): bigint {
	if (value === null || value === undefined || value === "") return fallback
	const n =
		typeof value === "string"
			? Number(value.replace(/[^0-9.-]/g, ""))
			: Number(value)
	if (!Number.isFinite(n)) return fallback
	try {
		return BigInt(Math.trunc(n))
	} catch {
		return fallback
	}
}

function pickCurrentStock(input: any): bigint {
	// Prefer explicit numeric fields
	if (input.stock && String(input.stock).trim() !== "")
		return toBigInt(input.stock)
	// Look into Product Specifications
	const specs: Array<{ name: string; value: any }> =
		input["Product Specifications"] || []
	const found = specs.find((s) => /existencias|stock/i.test(s.name || ""))
	if (found) return toBigInt(found.value)
	return BigInt(0)
}

function mapAttributes(input: any): Array<{ name: string; value: string }> {
	const variations = Array.isArray(input.variations) ? input.variations : []
	const chosen = Array.isArray(input.product_variation)
		? input.product_variation
		: []
	if (
		variations.length &&
		chosen.length &&
		typeof chosen[0]?.value === "string"
	) {
		const chosenValues = String(chosen[0].value)
			.split(",")
			.map((s: string) => s.trim())
		return variations.map((v: any, idx: number) => ({
			name: v.name || `Option ${idx + 1}`,
			value:
				chosenValues[idx] ||
				(Array.isArray(v.variations) ? v.variations[0] : ""),
		}))
	}
	// Fallback: take first option of each variation
	if (variations.length) {
		return variations.map((v: any, idx: number) => ({
			name: v.name || `Option ${idx + 1}`,
			value:
				Array.isArray(v.variations) && v.variations.length
					? String(v.variations[0])
					: "",
		}))
	}
	return []
}

async function upsertBrand(brandName?: string): Promise<bigint> {
	const name = (brandName && String(brandName).trim()) || "Unknown"
	const code = slugify(name)
	const brand = await prisma.brand.upsert({
		where: { code },
		update: { name },
		create: { code, name, description: name },
	})
	return BigInt(brand.id)
}

async function upsertCategoryFromBreadcrumb(
	breadcrumb?: string[]
): Promise<bigint> {
	// Use the last breadcrumb element as category to avoid uniqueness conflicts across hierarchy
	const leaf =
		Array.isArray(breadcrumb) && breadcrumb.length
			? breadcrumb[breadcrumb.length - 1]
			: "General"
	const name = String(leaf || "General")
	const existed = await prisma.productCategory.findUnique({ where: { name } })
	if (existed) return BigInt(existed.id)
	const created = await prisma.productCategory.create({
		data: { name, description: name, parent_id: null },
	})
	return BigInt(created.id)
}

async function createPromotionsFromVouchers(
	vouchers: InputProduct1["vouchers"],
	spuId: bigint,
	accountId: bigint,
	currency: string
): Promise<void> {
	if (!vouchers || !Array.isArray(vouchers) || vouchers.length === 0) {
		return
	}

	for (const voucher of vouchers) {
		if (!voucher) continue

		// Skip if voucher is not valid (expired or invalid dates)
		try {
			const validityEnd = new Date(voucher.validity_end)
			const validityStart = new Date(voucher.validity_start)
			if (isNaN(validityEnd.getTime()) || isNaN(validityStart.getTime())) {
				continue
			}
			// Skip expired vouchers
			if (validityEnd < new Date()) {
				continue
			}
		} catch {
			continue
		}

		// Generate promotion code from text_info or create unique code
		const promotionCode =
			voucher.text_info && voucher.text_info.trim()
				? `VOUCHER_${voucher.text_info.trim().toUpperCase()}_${uuidv4().substring(0, 8)}`
				: `PROMO_${uuidv4()}`

		// Determine discount type
		const discountAmount = voucher.discount || 0
		const discountPercent = voucher.discount_percentage || 0
		const minSpend = toBigInt(voucher.min_spend, BigInt(0))
		const discountCap = toBigInt(voucher.discount_cap, BigInt(0))

		// Create promotion title
		const title = voucher.shop_vouncher || "Special Promotion"

		// Create promotion first
		const promotion = await prisma.promotion.create({
			data: {
				code: promotionCode,
				owner_id: accountId,
				type: "Discount", // PromotionType.Discount
				title: title,
				description: `Promotion code: ${voucher.text_info || promotionCode}`,
				is_active: true,
				auto_apply: voucher.claimable === false, // If not claimable, auto-apply
				date_started: new Date(voucher.validity_start),
				date_ended: new Date(voucher.validity_end),
				refs: {
					create: {
						ref_type: "ProductSpu",
						ref_id: spuId,
					},
				},
			},
		})

		// Create discount with promotion id (one-to-one relationship)
		await prisma.promotionDiscount.create({
			data: {
				id: promotion.id,
				min_spend: minSpend,
				max_discount: discountCap,
				discount_percent: discountPercent > 0 ? discountPercent : null,
				discount_price: discountAmount > 0 && discountPercent === 0 ? toBigInt(discountAmount) : null,
			},
		})
	}
}

async function main(inputs: InputProduct1[] = []) {
	const account = await prisma.account.create({
		data: {
			type: "Vendor",
			email: `vendor+${uuidv4()}@example.com`,
			password: "password123",
			vendor_profile: {
				create: {},
			},
		},
	})

	// Initialize progress bar
	const progressBar = new cliProgress.SingleBar(
		{
			format: "Progress |{bar}| {percentage}% | {value}/{total} products | ETA: {eta}s | {title}",
			barCompleteChar: "\u2588",
			barIncompleteChar: "\u2591",
			hideCursor: true,
		},
		cliProgress.Presets.shades_classic
	)

	const totalProducts = inputs.length
	progressBar.start(totalProducts, 0, { title: "Starting..." })

	let processedCount = 0

	for (const input of inputs) {
		// Update progress bar with current product title
		const displayTitle =
			input.title && input.title.length > 50
				? input.title.substring(0, 50) + "..."
				: input.title || "Unnamed Product"

		try {
			progressBar.update(processedCount, { title: displayTitle })

			const brandId = await upsertBrand(input.brand)
			const categoryId = await upsertCategoryFromBreadcrumb(input.breadcrumb)

			// Translate product name to English
			const originalTitle = input.title || "Unnamed Product"
			const translatedTitle = await translateToEnglish(originalTitle)

			const spu = await prisma.productSpu.create({
				data: {
					account_id: account.id,
					code: slugify(originalTitle || "product") + "." + uuidv4(),
					category_id: categoryId,
					brand_id: brandId,
					name: translatedTitle, // Use translated name
					description: (
						input["Product Description"] ||
						""
					).toString(),
					is_active: String(input.is_available || "").toLowerCase() !== "false",
					featured_sku_id: null, // to be updated later

					products: {
						create: {
							price: toBigInt(input.final_price ?? input.initial_price),
							can_combine: false,
							date_created: new Date(),
							// Prisma schema now stores attributes directly as Json on ProductSku
							attributes: mapAttributes(input),
						},
					},
				},
				include: { products: true },
			})

			const product = spu.products[0]

			// Update featured_sku_id
			await prisma.productSpu.update({
				where: { id: spu.id },
				data: { featured_sku_id: product.id },
			})

			await prisma.stock.upsert({
				where: {
					ref_id_ref_type: { ref_id: product.id, ref_type: "ProductSku" },
				},
				update: {
					current_stock: pickCurrentStock(input),
					sold: toBigInt(input.sold),
				},
				create: {
					ref_id: product.id,
					ref_type: "ProductSku",
					current_stock: pickCurrentStock(input),
					sold: toBigInt(input.sold),
					date_created: new Date(),
				},
			})

			const images: string[] = Array.isArray(input.image)
				? input.image.slice(0, 10)
				: []
			let order = 0
			for (const url of images) {
				if (!url || typeof url !== "string") continue
				await prisma.resourceReference.create({
					data: {
						ref_id: spu.id,
						ref_type: "ProductSpu",
						is_primary: order === 0,
						order: order++,
						resource: {
							connectOrCreate: {
								create: {
									id: uuidv4(),
									provider: "remote",
									metadata: Prisma.JsonNull,
									object_key: url,
									uploaded_by: account.id,
									status: "Success",
									mime: url.includes(".png") ? "image/png" : "image/jpeg",
									size: BigInt(0),
								},
								where: {
									provider_object_key: {
										provider: "remote",
										object_key: url,
									},
								},
							},
						},
					},
				})
			}

			await prisma.searchSync.create({
				data: {
					ref_type: "Product",
					ref_id: spu.id,
					is_stale_embedding: true,
					is_stale_metadata: true,
					date_created: new Date(),
				},
			})

			// Create promotions from vouchers
			await createPromotionsFromVouchers(
				input.vouchers,
				spu.id,
				account.id,
				input.currency || "THB"
			)

			// Update progress bar after processing
			processedCount++
			progressBar.update(processedCount, { title: displayTitle })
		} catch (error) {
			console.error(`\nError processing product: ${input.title || "Unknown"}`)
			console.error(error)
			processedCount++
			progressBar.update(processedCount, {
				title: `Error: ${displayTitle.substring(0, 30)}`,
			})
		}
	}

	// Complete progress bar
	progressBar.update(totalProducts, { title: "Completed!" })
	progressBar.stop()
	console.log(`\n✅ Successfully processed ${processedCount}/${totalProducts} products`)
}

const inputs: InputProduct1[] = JSON.parse(
	fs.readFileSync("data.json", "utf-8")
)

main(inputs)
	.catch((e) => console.error(e))
	.finally(async () => {
		await prisma.$disconnect()
	})
