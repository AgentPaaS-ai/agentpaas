#!/usr/bin/env python3
"""
Generate a single icon using Bonsai Image Ternary 4B (via mflux Flux2Klein).
Usage: python3 generate_icon.py --prompt "..." --output /path/to/icon.png [--seed 42]
"""
import argparse, os, sys

# Ensure mflux venv is activated
sys.path.insert(0, os.path.expanduser("~/venvs/mflux/lib/python3.12/site-packages"))

from mflux.models.common.config.model_config import ModelConfig
from mflux.models.flux2.variants import Flux2Klein
from mflux.models.flux2.latent_creator.flux2_latent_creator import Flux2LatentCreator
from mflux.utils.dimension_resolver import DimensionResolver
from mflux.utils.prompt_util import PromptUtil
from mflux.utils.image_util import ImageUtil
from mflux.callbacks.callback_manager import CallbackManager

BONSAI_PATH = os.path.expanduser("~/.cache/mflux/bonsai")

def main():
    parser = argparse.ArgumentParser(description="Generate icon with Bonsai model")
    parser.add_argument("--prompt", required=True, help="Text prompt")
    parser.add_argument("--output", required=True, help="Output PNG path")
    parser.add_argument("--seed", type=int, default=42, help="Random seed")
    parser.add_argument("--width", type=int, default=256, help="Image width")
    parser.add_argument("--height", type=int, default=256, help="Image height")
    parser.add_argument("--steps", type=int, default=4, help="Inference steps")
    parser.add_argument("--guidance", type=float, default=1.0, help="Guidance scale")
    args = parser.parse_args()

    # Create model config pointing to the bonsai local path
    model_config = ModelConfig(
        aliases=["bonsai"],
        model_name="prism-ml/bonsai-image-ternary-4B-mlx-2bit",
        base_model="black-forest-labs/FLUX.2-klein-4B",
        controlnet_model=None,
        custom_transformer_model=None,
        num_train_steps=1000,
        max_sequence_length=512,
        supports_guidance=False,
        requires_sigma_shift=False,
        priority=50,
        transformer_overrides=None,
    )

    print(f"Loading Bonsai model from {BONSAI_PATH}...")
    print(f"RAM before load: ", end="")
    os.system("vm_stat | awk '/Pages active/ {printf \"%.1f GB active\\n\", $NF*16384/1073741824}'")

    model = Flux2Klein(
        model_config=model_config,
        quantize=None,
        model_path=BONSAI_PATH,
    )

    memory_saver = CallbackManager.register_callbacks(
        args=type('Args', (), {
            'low_ram': True,
            'mlx_cache_limit_gb': 4,
            'battery_percentage_stop_limit': 5,
            'auto_seeds': None,
            'seed': [args.seed],
            'stepwise_image_output_dir': None,
        })(),
        model=model,
        latent_creator=Flux2LatentCreator,
    )

    width, height = DimensionResolver.resolve(
        width=args.width,
        height=args.height,
        reference_image_path=None,
    )

    print(f"Generating {width}x{height} at {args.steps} steps...")
    image = model.generate_image(
        seed=args.seed,
        prompt=args.prompt,
        width=width,
        height=height,
        guidance=args.guidance,
        image_path=None,
        num_inference_steps=args.steps,
        image_strength=None,
        scheduler="flow_match_euler_discrete",
    )

    os.makedirs(os.path.dirname(args.output), exist_ok=True)
    ImageUtil.save_image(image=image, path=args.output, export_json_metadata=False)
    file_size = os.path.getsize(args.output)
    print(f"✅ Saved: {args.output} ({file_size/1024:.1f} KB)")

    if memory_saver:
        print(memory_saver.memory_stats())

if __name__ == "__main__":
    main()