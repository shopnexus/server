import json
import time
import logging
import numpy as np
from datetime import datetime
from typing import Dict, List, Optional
from flask import Flask, request, jsonify
from pymilvus import (
    connections,
    utility,
    FieldSchema,
    CollectionSchema,
    DataType,
    Collection,
    AnnSearchRequest,
    WeightedRanker,
)
from pymilvus.model.hybrid import MGTEEmbeddingFunction

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class UnifiedMilvusService:
    def __init__(self, milvus_host: str = "localhost", milvus_port: int = 19530):
        """Initialize unified service with Milvus connection"""
        self.milvus_host = milvus_host
        self.milvus_port = milvus_port

        # Event weights for user vector calculation (optimized for typical user behavior)
        self.event_weights = {
            "view": 0.1,  # Most common action, lower weight
            "add_to_cart": 0.3,  # Moderate engagement
            "purchase": 0.6,  # Strongest signal, highest weight
            "rating": 0.2  # Explicit feedback, moderate weight
        }
        self.update_weight = 0.5  # More aggressive blending to adapt faster to user behavior changes

        # Setup Milvus and collections
        self._setup_milvus()

    def _setup_milvus(self):
        """Setup Milvus connection and create collections"""
        try:
            connections.connect(host=self.milvus_host, port=self.milvus_port)
            logger.info("Connected to Milvus")

            # Initialize embedding function
            self.ef = MGTEEmbeddingFunction(use_fp16=False, device="cpu")
            self.dense_dim = self.ef.dim["dense"]

            # Setup collections
            self._setup_products_collection()
            self._setup_customer_collection()

        except Exception as e:
            logger.error(f"Failed to setup Milvus: {e}")
            raise

    def _setup_products_collection(self):
        """Setup products collection for search"""
        collection_name = "products"

        if not utility.has_collection(collection_name):
            fields = [
                FieldSchema(name="id", dtype=DataType.INT64, is_primary=True, auto_id=False),
                FieldSchema(name="text", dtype=DataType.VARCHAR, max_length=4048),
                FieldSchema(name="sparse_vector", dtype=DataType.SPARSE_FLOAT_VECTOR),
                FieldSchema(name="dense_vector", dtype=DataType.FLOAT_VECTOR, dim=self.dense_dim),
            ]
            schema = CollectionSchema(fields)
            self.products_collection = Collection(name=collection_name, schema=schema)

            # Create indexes
            self.products_collection.create_index("sparse_vector", {
                "index_type": "SPARSE_INVERTED_INDEX",
                "metric_type": "IP"
            })
            self.products_collection.create_index("dense_vector", {
                "index_type": "AUTOINDEX",
                "metric_type": "COSINE"
            })
            logger.info(f"Created collection: {collection_name}")
        else:
            self.products_collection = Collection(collection_name)
            logger.info(f"Connected to existing collection: {collection_name}")

        self.products_collection.load()

    def _setup_customer_collection(self):
        """Setup customer collection for user vectors"""
        collection_name = "customer"

        if not utility.has_collection(collection_name):
            fields = [
                FieldSchema(name="account_id", dtype=DataType.INT64, is_primary=True, auto_id=False),
                FieldSchema(name="dense_vector", dtype=DataType.FLOAT_VECTOR, dim=self.dense_dim),
                FieldSchema(name="last_updated", dtype=DataType.VARCHAR, max_length=50),
                FieldSchema(name="event_count", dtype=DataType.INT64),
            ]
            schema = CollectionSchema(fields, description="Customer preference vectors")
            self.customer_collection = Collection(name=collection_name, schema=schema)

            self.customer_collection.create_index("dense_vector", {
                "index_type": "AUTOINDEX",
                "metric_type": "COSINE"
            })
            logger.info(f"Created collection: {collection_name}")
        else:
            self.customer_collection = Collection(collection_name)

            logger.info(f"Connected to existing collection: {collection_name}")

        self.customer_collection.load()

    def hybrid_search(self, dense_vec, sparse_vec, dense_weight=1.0, sparse_weight=1.0, offset=0, limit=10):
        """Hybrid search in products collection"""
        dense_req = AnnSearchRequest([dense_vec], "dense_vector", {"metric_type": "COSINE"}, limit=limit)
        sparse_req = AnnSearchRequest([sparse_vec], "sparse_vector", {"metric_type": "IP"}, limit=limit)
        rerank = WeightedRanker(sparse_weight, dense_weight)

        return self.products_collection.hybrid_search(
            [sparse_req, dense_req],
            rerank=rerank,
            limit=limit,
            output_fields=["id"],
            **{
                "offset": offset
            })[0]

    def dense_search(self, dense_vec, offset=0, limit=10):
        """Dense vector search in products collection"""
        search_params = {"metric_type": "COSINE", "params": {"nprobe": 10}}

        return self.products_collection.search(
            data=[dense_vec],
            anns_field="dense_vector",
            param=search_params,
            limit=limit,
            output_fields=["id"],
            **{
                "offset": offset
            }
        )[0]

    def calculate_user_vector(self, events: List[Dict]) -> Optional[np.ndarray]:
        """Calculate user preference vector from events by fetching product embeddings"""
        if not events:
            return None

        start_time = time.time()

        # Collect product IDs and their preference scores
        preference_start = time.time()
        product_preferences = {}

        for event in events:
            ref_id = event.get('ref_id')
            event_type = event.get('event_type')

            if not ref_id or not event_type:
                continue

            weight = self.event_weights.get(event_type, 0.1)
            metadata_weight = 1.0

            if 'metadata' in event:
                metadata = event['metadata']
                if 'quantity' in metadata:
                    metadata_weight *= min(metadata['quantity'] / 5.0, 2.0)
                if 'price' in metadata:
                    metadata_weight *= min(metadata['price'] / 1000000, 1.5)

            if ref_id not in product_preferences:
                product_preferences[ref_id] = 0.0
            product_preferences[ref_id] += weight * metadata_weight

        preference_time = time.time() - preference_start

        if not product_preferences:
            return None

        # Fetch dense vectors for all products from products collection
        fetch_start = time.time()
        product_ids = list(product_preferences.keys())
        product_vectors = self._get_product_vectors(product_ids)
        fetch_time = time.time() - fetch_start

        if not product_vectors:
            logger.warning(f"No product vectors found for products: {product_ids}")
            return None

        # Create weighted user preference vector
        vector_start = time.time()
        user_vector = np.zeros(self.dense_dim)
        total_weight = 0.0

        for product_id, preference_score in product_preferences.items():
            if product_id in product_vectors:
                product_vector = product_vectors[product_id]
                user_vector += preference_score * product_vector
                total_weight += preference_score

        # Normalize the vector
        if total_weight > 0:
            user_vector = user_vector / total_weight

        vector_time = time.time() - vector_start
        total_time = time.time() - start_time

        logger.debug(f"User vector calculation: {len(product_preferences)} products, "
                     f"{len(product_vectors)} vectors fetched in {total_time:.3f}s "
                     f"(preference: {preference_time:.3f}s, fetch: {fetch_time:.3f}s, vector: {vector_time:.3f}s)")

        return user_vector

    def _get_product_vectors(self, product_ids: List[int]) -> Dict[int, np.ndarray]:
        """Fetch dense vectors for given product IDs from products collection"""
        try:
            if not product_ids:
                return {}

            # Create expression to filter by product IDs
            id_list = ','.join(map(str, product_ids))
            expr = f"id in [{id_list}]"

            # Use query instead of search for better performance
            results = self.products_collection.query(
                expr=expr,
                output_fields=["id", "dense_vector"],
                limit=len(product_ids)
            )

            product_vectors = {}
            if results:
                for entity in results:
                    product_id = entity.get('id')
                    dense_vector = np.array(entity.get('dense_vector'))
                    product_vectors[product_id] = dense_vector

            logger.info(f"Fetched {len(product_vectors)} product vectors out of {len(product_ids)} requested")
            return product_vectors

        except Exception as e:
            logger.error(f"Error fetching product vectors: {e}")
            return {}

    def get_user_vector(self, account_id: int) -> Optional[np.ndarray]:
        """Get existing user vector from Milvus using query instead of search"""
        try:
            # Use query instead of search for better performance
            results = self.customer_collection.query(
                expr=f"account_id == {account_id}",
                output_fields=["dense_vector"],
                limit=1
            )

            if results and len(results) > 0:
                return np.array(results[0]['dense_vector'])
            return None

        except Exception as e:
            logger.error(f"Error retrieving user vector for account {account_id}: {e}")
            return None

    def update_user_vector(self, account_id: int, new_vector: np.ndarray, event_count: int):
        """Update user vector with blending"""
        try:
            existing_vector = self.get_user_vector(account_id)

            if existing_vector is not None:
                blended_vector = (1 - self.update_weight) * existing_vector + self.update_weight * new_vector
            else:
                blended_vector = new_vector

            entities = [
                [account_id],
                [blended_vector.tolist()],
                [datetime.now().isoformat()],
                [event_count]
            ]

            self.customer_collection.upsert(entities)
            # Remove flush() - it's causing the slowdown
            # self.customer_collection.flush()
            logger.info(f"Updated user vector for account {account_id}")

        except Exception as e:
            logger.error(f"Error updating user vector for account {account_id}: {e}")

    def process_events_batch(self, events: List[Dict]):
        """Process events and update user vectors"""
        start_time = time.time()

        # Group events by account_id
        group_start = time.time()
        account_events = {}
        for event in events:
            account_id = event.get('account_id')
            if account_id:
                if account_id not in account_events:
                    account_events[account_id] = []
                account_events[account_id].append(event)
        group_time = time.time() - group_start

        # Process each account's events
        process_start = time.time()
        processed_accounts = 0
        total_events_processed = 0

        for account_id, user_events in account_events.items():
            try:
                # Calculate new user vector
                vector_start = time.time()
                new_vector = self.calculate_user_vector(user_events)
                vector_time = time.time() - vector_start

                if new_vector is not None:
                    # Update user vector
                    update_start = time.time()
                    self.update_user_vector(account_id, new_vector, len(user_events))
                    update_time = time.time() - update_start

                    processed_accounts += 1
                    total_events_processed += len(user_events)

                    logger.info(f"Account {account_id}: {len(user_events)} events processed "
                                f"(vector: {vector_time:.3f}s, update: {update_time:.3f}s)")
                else:
                    logger.warning(f"Account {account_id}: Failed to calculate user vector")

            except Exception as e:
                logger.error(f"Error processing events for account {account_id}: {e}")

        process_time = time.time() - process_start

        # Flush all updates at once for better performance
        flush_start = time.time()
        self.customer_collection.flush()
        flush_time = time.time() - flush_start

        total_time = time.time() - start_time

        logger.info(f"Batch processing completed: {processed_accounts} accounts, "
                    f"{total_events_processed} events in {process_time:.3f}s "
                    f"(grouping: {group_time:.3f}s, processing: {process_time:.3f}s, flush: {flush_time:.3f}s, total: {total_time:.3f}s)")


# Initialize service
service = UnifiedMilvusService()

# Flask app
app = Flask(__name__)


@app.route("/search", methods=["POST"])
def search():
    """Product search endpoint"""
    start = time.time()
    data = request.json
    query = data.get("query")
    if not query:
        return jsonify({"error": "Missing query"}), 400

    limit = data.get("limit", 10)
    weights = data.get("weights", {"dense": 1.0, "sparse": 1.0})
    offset = data.get("offset", 0)

    query_embeddings = service.ef([query])
    logger.info(f"Embedding time: {(time.time() - start) * 1000:.2f} ms")

    result = service.hybrid_search(
        query_embeddings["dense"][0],
        query_embeddings["sparse"][[0]],
        dense_weight=weights.get("dense", 1.0),
        sparse_weight=weights.get("sparse", 1.0),
        offset=offset,
        limit=limit,
    )

    return jsonify([{"id": hit["id"], "score": hit.score} for hit in result])


@app.route("/analytics/process", methods=["POST"])
def process_analytics():
    """Process analytics events endpoint"""
    start_time = time.time()
    data = request.json
    events = data.get("events", [])

    if not events:
        return jsonify({"error": "No events provided"}), 400

    # Time the processing
    process_start = time.time()
    service.process_events_batch(events)
    process_time = time.time() - process_start

    total_time = time.time() - start_time

    logger.info(
        f"Analytics processing completed: {len(events)} events in {process_time:.3f}s (total: {total_time:.3f}s)")

    return jsonify({
        "message": f"Processed {len(events)} events",
        "processed_at": datetime.now().isoformat(),
        "performance": {
            "events_count": len(events),
            "processing_time_seconds": round(process_time, 3),
            "total_time_seconds": round(total_time, 3),
            "events_per_second": round(len(events) / process_time, 2) if process_time > 0 else 0
        }
    })


@app.route("/user/<int:account_id>/recommendations", methods=["GET"])
def get_recommendations(account_id):
    """Get product recommendations for a user based on their preferences"""
    limit = request.args.get("limit", 10, type=int)

    try:
        # Get user's preference vector
        user_vector = service.get_user_vector(account_id)
        if user_vector is None:
            # return jsonify({
            #     "error": f"No preference data found for user {account_id}",
            #     "message": "User needs to interact with products first"
            # }), 404
            return jsonify([])

        # Dense search for similar products using user's preference vector
        results = service.dense_search(user_vector.tolist(), limit=limit)

        return jsonify([{"id": hit["id"], "score": float(hit.score)} for hit in results])

    except Exception as e:
        logger.error(f"Error getting recommendations for user {account_id}: {e}")
        return jsonify({"error": "Failed to get recommendations"}), 500


@app.route("/health", methods=["GET"])
def health_check():
    """Health check endpoint"""
    return jsonify({
        "status": "healthy",
        "timestamp": datetime.now().isoformat(),
        "collections": {
            "products": service.products_collection.num_entities,
            "customer": service.customer_collection.num_entities
        }
    })


def main():
    """Main function - run as Flask API server"""
    app.run(host="0.0.0.0", port=8000, debug=True)


if __name__ == "__main__":
    main()
