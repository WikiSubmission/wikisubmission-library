export type WFileResult = {
    folder: string;
    file: string;
    path: string;
    extension: string;
    score: number;
    url: string;
    urls: { provider: string; url: string }[];
};