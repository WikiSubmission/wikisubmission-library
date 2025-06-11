export type WFileResult = {
    name: string;
    folder: string;
    path: string;
    type: string;
    score: number;
    url: string;
    urls: { provider: string; url: string }[];
};