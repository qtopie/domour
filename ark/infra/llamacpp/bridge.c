//go:build llamacpp

#include "bridge.h"
#include <llama.h>
#include <stdlib.h>
#include <string.h>

struct domour_llama {
    struct llama_model * model;
    struct llama_context * ctx;
    const struct llama_vocab * vocab;
    int32_t n_vocab;
};

void domour_llama_backend_init(void) {
    llama_backend_init();
}

void domour_llama_backend_free(void) {
    llama_backend_free();
}

domour_llama_t* domour_llama_load(const char* model_path, uint32_t n_ctx, int32_t n_threads) {
    struct llama_model_params mparams = llama_model_default_params();
    struct llama_model * model = llama_model_load_from_file(model_path, mparams);
    if (!model) {
        return NULL;
    }

    struct llama_context_params cparams = llama_context_default_params();
    if (n_ctx > 0) {
        cparams.n_ctx = n_ctx;
    }
    if (n_threads > 0) {
        cparams.n_threads = n_threads;
    }

    struct llama_context * ctx = llama_init_from_model(model, cparams);
    if (!ctx) {
        llama_model_free(model);
        return NULL;
    }

    const struct llama_vocab * vocab = llama_model_get_vocab(model);
    int32_t n_vocab = llama_vocab_n_tokens(vocab);

    domour_llama_t * dl = (domour_llama_t*)malloc(sizeof(domour_llama_t));
    if (!dl) {
        llama_free(ctx);
        llama_model_free(model);
        return NULL;
    }
    dl->model = model;
    dl->ctx = ctx;
    dl->vocab = vocab;
    dl->n_vocab = n_vocab;
    return dl;
}

void domour_llama_free(domour_llama_t* dl) {
    if (!dl) return;
    if (dl->ctx) {
        llama_free(dl->ctx);
        dl->ctx = NULL;
    }
    if (dl->model) {
        llama_model_free(dl->model);
        dl->model = NULL;
    }
    free(dl);
}

int32_t domour_llama_tokenize(domour_llama_t* dl, const char* text, int32_t text_len, int32_t* out_tokens, int32_t max_tokens) {
    if (!dl || !dl->vocab) return -1;
    return llama_tokenize(dl->vocab, text, text_len, (llama_token*)out_tokens, max_tokens, true, true);
}

int32_t domour_llama_token_to_piece(domour_llama_t* dl, int32_t token, char* buf, int32_t max_len) {
    if (!dl || !dl->vocab || !buf || max_len <= 0) return 0;
    int32_t n = llama_token_to_piece(dl->vocab, (llama_token)token, buf, max_len, 0, false);
    if (n < 0) return 0;
    if (n >= max_len) n = max_len - 1;
    buf[n] = '\0';
    return n;
}

int32_t domour_llama_get_vocab_size(domour_llama_t* dl) {
    if (!dl) return 0;
    return dl->n_vocab;
}

int32_t domour_llama_get_eos(domour_llama_t* dl) {
    if (!dl || !dl->vocab) return -1;
    return (int32_t)llama_vocab_eos(dl->vocab);
}

int32_t domour_llama_get_eot(domour_llama_t* dl) {
    if (!dl || !dl->vocab) return -1;
    return (int32_t)llama_vocab_eot(dl->vocab);
}

int32_t domour_llama_forward(domour_llama_t* dl, const int32_t* tokens, int32_t n_tokens, const int32_t* marker_indices, int32_t n_markers) {
    if (!dl || !dl->ctx || n_tokens <= 0) return -1;

    struct llama_batch batch = llama_batch_init(n_tokens, 0, 1);
    batch.n_tokens = n_tokens;
    for (int32_t i = 0; i < n_tokens; i++) {
        batch.token[i] = (llama_token)tokens[i];
        batch.pos[i] = i;
        batch.n_seq_id[i] = 1;
        batch.seq_id[i][0] = 0;
        batch.logits[i] = 0;
    }

    if (n_markers > 0 && marker_indices) {
        for (int32_t m = 0; m < n_markers; m++) {
            int32_t idx = marker_indices[m];
            if (idx >= 0 && idx < n_tokens) {
                batch.logits[idx] = 1;
            }
        }
    } else {
        batch.logits[n_tokens - 1] = 1;
    }

    int32_t ret = llama_decode(dl->ctx, batch);
    llama_batch_free(batch);
    return ret;
}

int32_t domour_llama_decode_step(domour_llama_t* dl, int32_t token, int32_t pos) {
    if (!dl || !dl->ctx) return -1;

    struct llama_batch batch = llama_batch_init(1, 0, 1);
    batch.n_tokens = 1;
    batch.token[0] = (llama_token)token;
    batch.pos[0] = (llama_pos)pos;
    batch.n_seq_id[0] = 1;
    batch.seq_id[0][0] = 0;
    batch.logits[0] = 1; // Always compute logits for the newly sampled token

    int32_t ret = llama_decode(dl->ctx, batch);
    llama_batch_free(batch);
    return ret;
}

const float* domour_llama_get_logits(domour_llama_t* dl, int32_t idx) {
    if (!dl || !dl->ctx) return NULL;
    return llama_get_logits_ith(dl->ctx, idx);
}

void domour_llama_kv_clear(domour_llama_t* dl) {
    if (!dl || !dl->ctx) return;
    llama_memory_t mem = llama_get_memory(dl->ctx);
    llama_memory_clear(mem, true);
}
